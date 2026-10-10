package admin

import (
	"context"
	"fmt"
	"net/url"

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
		label := humanizeFieldName(name)
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
		control.Message = i18n.T(ctx, "admin.list.filter_too_many",
			"%s cannot be filtered here: more than %d related rows.", control.Label, maxFilterChoices)
		return control, nil, nil
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
