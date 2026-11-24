package admin

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// lookupPageSize is how many rows one lookup search returns. The server owns
// it; a host cannot raise it.
const lookupPageSize = 10

// ErrInvalidLookup is returned when a lookup field is set up wrongly: on a
// field that is not a foreign key, whose target is not registered with the
// admin, or with no usable search fields.
var ErrInvalidLookup = errors.New("admin: invalid lookup field")

// LookupOptions configure the lookup widget of one foreign key field.
type LookupOptions struct {
	// Search names the string fields of the related model a search matches.
	// Empty means the related model's own Options.Search.
	Search []string
}

// LookupRules is what the admin needs from a widget that searches the related
// model instead of listing it.
type LookupRules struct {
	Search []string
}

// LookupWidget is the optional extension a Widget implements when its foreign
// key field is chosen by searching the related model. The admin owns the
// search route and answers it; a widget cannot register one.
type LookupWidget interface {
	LookupRules() LookupRules
}

type lookupWidget struct{ options LookupOptions }

// Lookup returns the Widget that replaces a foreign key's select with a
// search over the related model, set via Options.Widgets, e.g.
// Options{Widgets: map[string]Widget{"AuthorID": admin.Lookup(admin.LookupOptions{})}}.
// The related model must be registered with the admin and have search fields,
// its own or given here. A foreign key without it keeps its select.
func Lookup(options LookupOptions) Widget { return lookupWidget{options: options} }

func (w lookupWidget) LookupRules() LookupRules {
	return LookupRules{Search: append([]string(nil), w.options.Search...)}
}

var lookupWidgetTemplate = template.Must(template.New("lookupWidget").Parse(
	`<label for="field-{{.Name}}" class="field-label">{{.Label}}</label>
{{if .HelpText}}<p class="field-help" id="field-{{.Name}}-help">{{.HelpText}}</p>{{end}}
<input id="field-{{.Name}}" type="text" name="{{.Name}}" value="{{.Value}}" class="input" data-tango-widget="lookup" data-lookup-url="{{.LookupURL}}"{{if .RelatedLabel}} data-lookup-label="{{.RelatedLabel}}"{{end}}{{if not .Required}} data-lookup-optional{{end}}{{if .Text.Active}} data-msg-remove="{{.Text.T "admin.lookup.remove" "Remove"}}" data-msg-results="{{.Text.T "admin.lookup.results" "{count} results. Use the up and down arrow keys to choose."}}" data-msg-none="{{.Text.T "admin.lookup.none" "No results."}}" data-msg-more="{{.Text.T "admin.lookup.more" "More results. Type to narrow."}}" data-msg-error="{{.Text.T "admin.lookup.error" "Could not load results."}}" data-msg-selected="{{.Text.T "admin.lookup.chosen" "{label} selected."}}" data-msg-cleared="{{.Text.T "admin.lookup.cleared" "Selection removed."}}"{{end}}` + ariaAttrs + `>
{{if .RelatedLabel}}<p class="hint-small" id="field-{{.Name}}-current">{{.CurrentLabel}} {{.RelatedLabel}}</p>{{end}}`))

type lookupWidgetData struct {
	FieldContext
	CurrentLabel string
	Text         widgetText
}

// Render is the control that works without JavaScript: a text input holding
// the related row's primary key, with the row's label beside it.
func (lookupWidget) Render(ctx context.Context, f FieldContext) template.HTML {
	return renderWidgetTemplate(lookupWidgetTemplate, lookupWidgetData{
		FieldContext: f,
		CurrentLabel: newWidgetText(ctx).T("admin.lookup.current", "Selected:"),
		Text:         newWidgetText(ctx),
	})
}

func (lookupWidget) Parse(ctx context.Context, f FieldContext, form FieldValues, dest reflect.Value) error {
	return foreignKeySelectWidget{}.Parse(ctx, f, form, dest)
}

// lookupField is one resolved lookup: the related model and how to search it.
type lookupField struct {
	target     ModelRegistration
	pk         model.FieldMeta
	search     []string
	labelField string
	orderBy    []string
}

// lookupFields maps a field name of one model to its resolved lookup.
type lookupFields map[string]lookupField

// resolveLookups finds the lookup fields of one admin model. It fails at
// startup, naming the field, when one cannot work.
func resolveLookups(adminReg *adminregistry.Registry, registration ModelRegistration) (lookupFields, error) {
	var found lookupFields
	for _, field := range registration.Model.Fields {
		widget, ok := registration.Options.Widgets[field.Name].(LookupWidget)
		if !ok {
			continue
		}
		if contains(registration.Options.ReadOnly, field.Name) && !contains(registration.Options.Filters, field.Name) {
			continue // a read-only field shows its value and is searched only as a filter
		}
		resolved, err := resolveLookup(adminReg, registration, field, widget.LookupRules())
		if err != nil {
			return nil, err
		}
		if found == nil {
			found = lookupFields{}
		}
		found[field.Name] = resolved
	}
	return found, nil
}

func resolveLookup(adminReg *adminregistry.Registry, registration ModelRegistration, field model.FieldMeta, rules LookupRules) (lookupField, error) {
	name := registration.Model.Name + "." + field.Name
	if field.ForeignKey == "" {
		return lookupField{}, fmt.Errorf("%w: %s is not a foreign key", ErrInvalidLookup, name)
	}
	target, ok := adminReg.Get(field.ForeignKey)
	if !ok {
		return lookupField{}, fmt.Errorf("%w: %s points at %s, which is not registered with the admin", ErrInvalidLookup, name, field.ForeignKey)
	}
	search := rules.Search
	if len(search) == 0 {
		search = target.Options.Search
	}
	if len(search) == 0 {
		return lookupField{}, fmt.Errorf("%w: %s needs search fields: set LookupOptions.Search or Options.Search on %s", ErrInvalidLookup, name, field.ForeignKey)
	}
	for _, searched := range search {
		if err := checkSearchField(target.Model, searched); err != nil {
			return lookupField{}, fmt.Errorf("%w: %s: %v", ErrInvalidLookup, name, err)
		}
	}
	pk, err := primaryKeyField(target.Model)
	if err != nil {
		return lookupField{}, fmt.Errorf("%w: %s: %v", ErrInvalidLookup, name, err)
	}
	return lookupField{
		target:     target,
		pk:         pk,
		search:     append([]string(nil), search...),
		labelField: target.Options.Label,
		orderBy:    lookupOrder(target.Options.Ordering, pk.Name),
	}, nil
}

func checkSearchField(meta model.ModelMeta, name string) error {
	for _, field := range meta.Fields {
		if field.Name != name {
			continue
		}
		if field.Type.Kind() != reflect.String {
			return fmt.Errorf("search field %q on %s is %s, and must be a string", name, meta.Name, field.Type.Kind())
		}
		return nil
	}
	return fmt.Errorf("search field %q is not a field of %s", name, meta.Name)
}

// lookupOrder is the target's ordering with its primary key last, so the same
// search always lists rows in the same order.
func lookupOrder(ordering []string, pk string) []string {
	order := append([]string(nil), ordering...)
	for _, name := range order {
		if strings.TrimPrefix(name, "-") == pk {
			return order
		}
	}
	return append(order, pk)
}

// lookupPath is the canonical URL of a model field's search route. The form
// hands it to the widget, and lookupRoutes registers exactly it.
func lookupPath(modelName, field string) string {
	return modelAdminPath(modelName) + "_lookup/" + field + "/"
}

// lookupRoutes returns the one search route a model needs: a GET to
// modelPath + "_lookup/{field}/". It is registered only when a field of the
// model is a lookup. The field is looked up after the session check, so an
// anonymous request learns nothing about which fields are lookups.
func lookupRoutes(modelPath string, store *db.Store, brand Branding, lookups lookupFields) tango.URLs {
	if len(lookups) == 0 {
		return nil
	}
	path := modelPath + "_lookup/{field}/"
	view := requireSession(store, chrome{}, lookupView(store, lookups))
	urls := tango.URLs{tango.Path(http.MethodGet, path, view, hiddenPage)}
	return append(urls, unhandledMethodRoutes(path, brand, http.MethodGet)...)
}

type lookupItem struct {
	Value string `json:"value"`
	Text  string `json:"text"`
}

type lookupAnswer struct {
	Items []lookupItem `json:"items"`
	More  bool         `json:"more"`
}

func lookupView(store *db.Store, lookups lookupFields) tango.View {
	return func(ctx *tango.Context) error {
		header := ctx.ResponseWriter().Header()
		header.Set("Cache-Control", "no-store")
		header.Set("X-Content-Type-Options", "nosniff")

		lookup, ok := lookups[ctx.Param("field")]
		if !ok {
			return ctx.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
		}
		answer, err := lookup.runSearch(ctx.Context(), store, ctx.Query("q"))
		if err != nil {
			slog.ErrorContext(ctx.Context(), "admin: lookup search failed", "model", lookup.target.Model.Name, "error", err)
			return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "lookup failed"})
		}
		return ctx.JSON(http.StatusOK, answer)
	}
}

// runSearch runs one bounded query: one page plus one row, to tell whether more
// exist. An empty q lists the first page.
func (l lookupField) runSearch(ctx context.Context, store *db.Store, q string) (lookupAnswer, error) {
	meta := l.target.Model
	query := db.Query{OrderBy: l.orderBy, Limit: lookupPageSize + 1}
	if q != "" {
		for _, field := range l.search {
			query.Any = append(query.Any, db.Condition{Field: field, Op: db.OpLike, Value: "%" + q + "%"})
		}
		if key, ok := integerKey(l.pk.Type, q); ok {
			query.Any = append(query.Any, db.Condition{Field: l.pk.Name, Op: db.OpEq, Value: key})
		}
	}
	destPtr := reflect.New(reflect.SliceOf(meta.Type))
	if err := store.List(ctx, meta, query, destPtr.Interface()); err != nil {
		return lookupAnswer{}, err
	}
	rows := destPtr.Elem()
	answer := lookupAnswer{Items: make([]lookupItem, 0, lookupPageSize), More: rows.Len() > lookupPageSize}
	for i := 0; i < rows.Len() && i < lookupPageSize; i++ {
		row := rows.Index(i)
		value := fmt.Sprint(row.FieldByName(l.pk.Name).Interface())
		answer.Items = append(answer.Items, lookupItem{Value: value, Text: relatedChoiceText(row, l.labelField, value)})
	}
	return answer, nil
}

// integerKey parses q as a value of an integer primary key type. Any other
// key type, or a q that is not a whole number of that type, matches nothing by
// key, and the search fields still apply.
func integerKey(typ reflect.Type, q string) (any, bool) {
	switch typ.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
	default:
		return nil, false
	}
	if _, err := strconv.ParseInt(q, 10, 64); err != nil {
		if _, uerr := strconv.ParseUint(q, 10, 64); uerr != nil {
			return nil, false
		}
	}
	key, err := keyValue(typ, "", q)
	if err != nil {
		return nil, false
	}
	return key, true
}

// withLookupCurrentRow fills the lookup field's context from the one row it
// points at, read by primary key. An unset key (empty or zero) shows an empty
// control. A read-only field shows "label (key)" in place of the key.
func withLookupCurrentRow(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, target string, fc FieldContext) FieldContext {
	if fc.Value == "0" {
		fc.Value = ""
	}
	if fc.Value == "" {
		return fc
	}
	label := currentRowLabel(ctx, store, models, adminReg, target, fc.Value)
	switch {
	case label == "":
	case fc.ReadOnly:
		fc.Value = label + " (" + fc.Value + ")"
	default:
		fc.RelatedLabel = label
	}
	return fc
}

// currentRowLabel is the label of the related row with primary key raw, read
// once by key. It is empty when the target has no Label, the key is malformed
// or names no row, or the read fails; the key itself is then all that is
// shown.
func currentRowLabel(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, target, raw string) string {
	labelField := labelFieldFor(adminReg, target)
	if labelField == "" {
		return ""
	}
	meta, ok := models.Get(target)
	if !ok {
		return ""
	}
	pk, err := primaryKeyField(meta)
	if err != nil {
		return ""
	}
	key, err := keyValue(pk.Type, pk.Name, raw)
	if err != nil {
		return "" // malformed: the field's own error reports it on save
	}
	row := reflect.New(meta.Type)
	if err := store.Get(ctx, meta, key, row.Interface()); err != nil {
		if !errors.Is(err, db.ErrNotFound) {
			slog.ErrorContext(ctx, "admin: reading the current row of a lookup field failed", "model", target, "error", err)
		}
		return ""
	}
	return formatFieldValue(row.Elem().FieldByName(labelField))
}
