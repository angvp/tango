package admin

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

func deleteView(store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, registration ModelRegistration, nav []navItem, brand Branding) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"
	pageChrome := chrome{Nav: nav, Active: meta.Name, Brand: brand}

	return func(ctx *tango.Context) error {
		pkField, err := primaryKeyField(meta)
		if err != nil {
			return err
		}

		pkValue, err := parsePKValue(pkField, ctx.Param("pk"))
		if err != nil {
			return notFound(ctx)
		}

		switch ctx.Request().Method {
		case http.MethodGet:
			instancePtr := reflect.New(meta.Type)
			if err := store.Get(ctx.Context(), meta, pkValue, instancePtr.Interface()); err != nil {
				if errors.Is(err, db.ErrNotFound) {
					return notFound(ctx)
				}
				return err
			}

			pk := fmt.Sprint(pkValue)
			cells := newListRenderer(ctx.Context(), store, models, adminReg, meta, registration.Options)
			rc, err := rowChrome(ctx, cells, pageChrome, sectionName(meta, registration.Options), basePath, basePath+url.PathEscape(pk)+"/", instancePtr.Elem(), crumb{Label: i18n.T(ctx.Context(), "admin.button.delete", "Delete")})
			if err != nil {
				return err
			}
			label, err := cells.identityOf(instancePtr.Elem())
			if err != nil {
				return err
			}
			return render(ctx, http.StatusOK, deleteTemplate, deletePageData{
				chrome:    rc,
				Singular:  displayNoun(meta, registration.Options),
				Label:     deleteLabel(label, pk, displayNoun(meta, registration.Options)),
				CSRFToken: csrfTokenFromRequest(ctx.Request()),
			})

		case http.MethodPost:
			if err := ctx.Request().ParseForm(); err != nil {
				return err
			}
			if !verifySessionCSRF(ctx) {
				return forbiddenCSRF(ctx)
			}
			if err := store.Delete(ctx.Context(), meta, pkValue); err != nil {
				if errors.Is(err, db.ErrNotFound) {
					return notFound(ctx)
				}
				if isForeignKeyViolation(err) {
					return deleteRefused(ctx, meta, registration.Options, basePath, nav, brand)
				}
				return err
			}
			setOutcome(ctx, "admin.outcome.deleted", displayNoun(meta, registration.Options))

			return ctx.Redirect(basePath)

		default:
			return methodNotAllowed(ctx)
		}
	}
}

// deleteLabel names a row in the confirmation: its display label, or the noun
// and primary key when the label is only the key.
func deleteLabel(label, pk, noun string) string {
	if label == "" || label == pk {
		return noun + " " + pk
	}
	return label
}

// deleteRefused answers a delete the database refused because other rows
// still reference the row. It is a 409 page with the reason, not an error.
func deleteRefused(ctx *tango.Context, meta model.ModelMeta, opts adminregistry.Options, basePath string, nav []navItem, brand Branding) error {
	noun := displayNoun(meta, opts)
	title := i18n.T(ctx.Context(), "admin.delete.title", "Delete this %s", noun)
	return render(ctx, http.StatusConflict, deleteTemplate, deletePageData{
		chrome:   chrome{Nav: nav, Active: meta.Name, Brand: brand}.withContext(ctx.Context()).withCrumbs(crumb{Label: sectionName(meta, opts), Href: basePath}, crumb{Label: title}),
		Singular: noun,
		Error:    i18n.T(ctx.Context(), "admin.delete.refused", "This %s cannot be deleted because other rows still use it. Remove or change those rows first.", noun),
	})
}
