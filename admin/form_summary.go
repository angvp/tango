package admin

import (
	"context"

	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// formSummary lists the problems of a form that failed to save, above its
// first field. Heading counts them in words; focus moves to it on render.
type formSummary struct {
	Heading string
	Items   []formSummaryItem
}

// formSummaryItem is one problem. A field problem links to its field and is
// prefixed with the field's label, so the link identifies the field even when
// the message does not name it; a problem that belongs to no field is plain
// text.
type formSummaryItem struct {
	Href string
	Text string
}

// buildFormSummary returns the summary of a failed save, or nil when the
// page needs none. One field problem keeps its message beside the field, so
// the summary appears for more than one problem, or for a single problem that
// is not tied to a field.
func buildFormSummary(ctx context.Context, fields []formField, formError string, noun string) *formSummary {
	var items []formSummaryItem
	for _, field := range fields {
		if field.Error == "" {
			continue
		}
		items = append(items, formSummaryItem{
			Href: "#field-" + field.Name,
			Text: i18n.T(ctx, "admin.form.problem_item", "%s: %s", field.Label, field.Error),
		})
	}
	if formError != "" {
		items = append(items, formSummaryItem{Text: formError})
	}
	if len(items) == 0 || (len(items) == 1 && formError == "") {
		return nil
	}

	heading := i18n.T(ctx, "admin.form.problems", "Fix %d problems to save this %s.", len(items), noun)
	if len(items) == 1 {
		heading = i18n.T(ctx, "admin.form.problem_one", "Fix 1 problem to save this %s.", noun)
	}
	return &formSummary{Heading: heading, Items: items}
}

// formPage assembles the data of a create or edit page, with its summary and
// the plural noun its cancel link names.
func formPage(ctx context.Context, page chrome, title string, meta model.ModelMeta, opts adminregistry.Options, fields []formField, formError, csrfToken string) formPageData {
	return formPageData{
		chrome:    page.withPageTitle(title),
		Title:     title,
		Fields:    fields,
		Summary:   buildFormSummary(ctx, fields, formError, displayNoun(meta, opts)),
		ListName:  pluralNoun(meta, opts),
		CSRFToken: csrfToken,
	}
}
