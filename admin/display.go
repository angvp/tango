package admin

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// DisplayFunc computes one changelist cell from a model row, which is passed
// as a pointer to the model value. Set it through Options.Display; see
// adminregistry.DisplayFunc.
type DisplayFunc = adminregistry.DisplayFunc

// Display is what one changelist cell shows: escaped Text, and an optional
// Href that is rendered only when it is a safe URL (see safeHref).
type Display = adminregistry.Display

// listCell is one rendered changelist cell. Badge marks a plain boolean
// value ("yes" or "no") for the badge styling; Href makes the cell a link.
type listCell struct {
	Text  string
	Href  string
	Badge string
}

// plainCell is a cell holding an ordinary formatted field value.
func plainCell(text string) listCell {
	cell := listCell{Text: text}
	switch text {
	case "true":
		cell.Badge = "yes"
	case "false":
		cell.Badge = "no"
	}
	return cell
}

// displayCell runs a host DisplayFunc for one row. A link with no text shows
// its own URL, so the cell is never an invisible link.
func displayCell(ctx context.Context, fn DisplayFunc, row reflect.Value) listCell {
	display := fn(ctx, row.Interface())
	cell := listCell{Text: display.Text}
	if href, ok := safeHref(display.Href); ok {
		cell.Href = href
		if cell.Text == "" {
			cell.Text = href
		}
	}
	return cell
}

// emptyCellText is shown in the changelist for an unset foreign key.
const emptyCellText = "—"

// listRenderer builds the cells of one changelist page's rows.
type listRenderer struct {
	ctx      context.Context
	store    *db.Store
	models   *model.Registry
	adminReg *adminregistry.Registry
	meta     model.ModelMeta
	display  map[string]DisplayFunc
	columns  []string
}

func newListRenderer(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, meta model.ModelMeta, opts adminregistry.Options) listRenderer {
	return listRenderer{ctx: ctx, store: store, models: models, adminReg: adminReg, meta: meta, display: opts.Display, columns: opts.ListDisplay}
}

// identityText is the label a row goes by: its first list column's text, or
// the key when that is blank.
func identityText(pk, firstCell string) string {
	if strings.TrimSpace(firstCell) != "" {
		return firstCell
	}
	return pk
}

// identityOf is the label of one row, the same text the changelist gives it,
// rendering only the first list column.
func (r listRenderer) identityOf(row reflect.Value) (string, error) {
	pkField, err := primaryKeyField(r.meta)
	if err != nil {
		return "", err
	}
	first := ""
	if len(r.columns) > 0 {
		first = r.cell(row, r.columns[0]).Text
	}
	return identityText(fmt.Sprint(row.FieldByName(pkField.Name).Interface()), first), nil
}

// cell renders column for one row. A Display function wins over the field
// of the same name; otherwise a foreign key shows its related label and any
// other field its formatted value.
func (r listRenderer) cell(row reflect.Value, column string) listCell {
	if fn, ok := r.display[column]; ok {
		return displayCell(r.ctx, fn, row.Addr())
	}
	fieldValue := row.FieldByName(column)
	for _, field := range r.meta.Fields {
		if field.Name == column && field.ForeignKey != "" {
			if fieldValue.IsZero() {
				return plainCell(emptyCellText)
			}
			return plainCell(relatedLabel(r.ctx, r.store, r.models, r.adminReg, field.ForeignKey, fieldValue.Interface()))
		}
	}
	return plainCell(formatFieldValue(fieldValue))
}

// listHeader is a column's header: its Labels entry when the host set one,
// otherwise the humanized name. A synthetic column has no other source, so
// registration requires its label.
func listHeader(name string, opts adminregistry.Options) string {
	if label := opts.Labels[name]; label != "" {
		return label
	}
	return humanizeFieldName(name)
}
