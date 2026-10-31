package admin

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// filterAny is the query value, and the select option, meaning "no filter".
const filterAny = "any"

// Boolean filter values, as spelled in the query string and the select.
const (
	filterYes = "yes"
	filterNo  = "no"
)

// filterOption is one <option> of a changelist filter's <select>.
type filterOption struct {
	Value    string
	Text     string
	Selected bool
}

// filterControl is one configured changelist filter as rendered. Options is
// empty, and Message set, when the filter has no choices to offer.
type filterControl struct {
	Key     string
	ID      string
	Label   string
	Options []filterOption
	Message string
	// Applied is the label of the related row an over-cap filter applies, so
	// the page can show and keep it although it offers no select.
	Applied      string
	AppliedValue string
}

// appliedFilter is one filter the request actually applied: the condition
// it adds to the query and the canonical value it contributes to page state.
type appliedFilter struct {
	Condition db.Condition
	Value     string
}

// listFilters is the changelist's filter state for one request. Where is
// ANDed with search; Params holds only the applied filters, for page links.
type listFilters struct {
	Where    []db.Condition
	Params   url.Values
	Controls []filterControl
}

// resolveListFilters reads every configured filter's query value through
// query and returns the applied ones. Unknown or malformed values, and
// foreign key IDs the related model does not have, are ignored rather than
// reported, so an operator's stale or hand-edited URL still renders.
func resolveListFilters(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, meta model.ModelMeta, opts adminregistry.Options, query func(string) string) (listFilters, error) {
	result := listFilters{Params: url.Values{}}
	for _, name := range opts.Filters {
		field, ok := findFieldMeta(meta, name)
		if !ok {
			return listFilters{}, fmt.Errorf("tango admin: filter field %q not found on %s", name, meta.Name)
		}
		label := filterLabel(field, name)
		if custom, ok := opts.Labels[name]; ok {
			label = custom
		}
		control, applied, err := resolveListFilter(ctx, store, models, adminReg, field, label, query(filterKey(name)))
		if err != nil {
			return listFilters{}, err
		}
		result.Controls = append(result.Controls, control)
		if applied != nil {
			result.Where = append(result.Where, applied.Condition)
			result.Params.Set(control.Key, applied.Value)
		}
	}
	return result, nil
}

// filterLabel is the label a filter shows when the host gives none. A foreign
// key names its relation, so its ID suffix is dropped: AuthorID reads "Author".
func filterLabel(field model.FieldMeta, name string) string {
	label := humanizeFieldName(name)
	if field.ForeignKey != "" {
		if trimmed := strings.TrimSuffix(label, " ID"); trimmed != "" {
			return trimmed
		}
	}
	return label
}

// resolveListFilter dispatches one filter to its kind's resolver.
func resolveListFilter(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, field model.FieldMeta, label, raw string) (filterControl, *appliedFilter, error) {
	control := filterControl{
		Key:   filterKey(field.Name),
		ID:    "filter-" + db.ColumnName(field.Name),
		Label: label,
	}
	if field.ForeignKey != "" {
		return foreignKeyFilter(ctx, store, models, adminReg, field, control, raw)
	}
	return booleanFilter(ctx, field, control, raw)
}

// booleanFilter offers any, yes and no. Only yes and no apply.
func booleanFilter(ctx context.Context, field model.FieldMeta, control filterControl, raw string) (filterControl, *appliedFilter, error) {
	selected := filterAny
	var applied *appliedFilter
	if raw == filterYes || raw == filterNo {
		selected = raw
		applied = &appliedFilter{
			Condition: db.Condition{Field: field.Name, Op: db.OpEq, Value: raw == filterYes},
			Value:     raw,
		}
	}

	control.Options = []filterOption{
		{Value: filterAny, Text: i18n.T(ctx, "admin.list.filter_any", "Any")},
		{Value: filterYes, Text: i18n.T(ctx, "admin.boolean.yes", "Yes")},
		{Value: filterNo, Text: i18n.T(ctx, "admin.boolean.no", "No")},
	}
	control.Options = markSelected(control.Options, selected)
	return control, applied, nil
}

// foreignKeyFilter offers any plus the related rows, in the same order as the
// foreign key select. A raw value applies only if it names one of those rows.
func foreignKeyFilter(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, field model.FieldMeta, control filterControl, raw string) (filterControl, *appliedFilter, error) {
	choices, overCap, err := loadFilterChoices(ctx, store, models, adminReg, field.ForeignKey)
	if err != nil {
		return filterControl{}, nil, err
	}
	if overCap {
		return overCapFilter(ctx, store, models, adminReg, field, control, raw)
	}

	options := []filterOption{{Value: filterAny, Text: i18n.T(ctx, "admin.list.filter_any", "Any")}}
	selected := filterAny
	var applied *appliedFilter
	for _, choice := range choices {
		options = append(options, filterOption{Value: choice.Value, Text: choice.Text})
		if choice.Value != raw {
			continue
		}
		selected = raw
		value, err := foreignKeyValue(field, choice.Value)
		if err != nil {
			return filterControl{}, nil, err
		}
		applied = &appliedFilter{
			Condition: db.Condition{Field: field.Name, Op: db.OpEq, Value: value},
			Value:     choice.Value,
		}
	}
	control.Options = markSelected(options, selected)
	return control, applied, nil
}

// overCapFilter handles a foreign key filter whose related model is too
// large to list. The cap limits what is offered, not what can be filtered: a
// value in the URL is looked up on its own and applied when the row exists.
func overCapFilter(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, field model.FieldMeta, control filterControl, raw string) (filterControl, *appliedFilter, error) {
	control.Message = i18n.T(ctx, "admin.list.filter_too_many",
		"%s has too many values to list (more than %d).", control.Label, maxFilterChoices)
	if raw == "" {
		return control, nil, nil
	}
	choice, found, err := loadFilterChoice(ctx, store, models, adminReg, field.ForeignKey, raw)
	if err != nil || !found {
		return control, nil, err
	}
	value, err := foreignKeyValue(field, choice.Value)
	if err != nil {
		return filterControl{}, nil, err
	}
	control.Applied = choice.Text
	control.AppliedValue = choice.Value
	return control, &appliedFilter{
		Condition: db.Condition{Field: field.Name, Op: db.OpEq, Value: value},
		Value:     choice.Value,
	}, nil
}

// filterSummary is one applied filter as the summary above the table names it:
// the filter's label and the text of the value it applies.
type filterSummary struct {
	Label string
	Value string
}

// appliedFilterSummary names each applied filter, in the order the filters
// are configured. A filter with no applied value is left out.
func appliedFilterSummary(controls []filterControl) []filterSummary {
	var summary []filterSummary
	for _, control := range controls {
		if text, ok := control.AppliedText(); ok {
			summary = append(summary, filterSummary{Label: control.Label, Value: text})
		}
	}
	return summary
}

// AppliedText is the text of the value the filter applies, and false when it
// applies none. An over-cap filter keeps the label of its row in Applied; the
// other filters show the text of their selected option, and "any" applies
// nothing.
func (c filterControl) AppliedText() (string, bool) {
	if c.AppliedValue != "" {
		return c.Applied, true
	}
	for _, option := range c.Options {
		if option.Selected && option.Value != filterAny {
			return option.Text, true
		}
	}
	return "", false
}

// markSelected returns options with the one whose Value is selected marked.
func markSelected(options []filterOption, selected string) []filterOption {
	marked := make([]filterOption, len(options))
	for i, option := range options {
		option.Selected = option.Value == selected
		marked[i] = option
	}
	return marked
}

// filterKey is the query parameter that carries the filter on field name.
func filterKey(name string) string {
	return "filter_" + db.ColumnName(name)
}

// findFieldMeta returns the metadata of the named field of meta.
func findFieldMeta(meta model.ModelMeta, name string) (model.FieldMeta, bool) {
	for _, field := range meta.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return model.FieldMeta{}, false
}

// hasCriteria reports whether the request narrows the list by search or filter.
func hasCriteria(search string, filters listFilters) bool {
	return search != "" || len(filters.Params) > 0
}
