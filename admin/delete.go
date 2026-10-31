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
			rc, err := rowChrome(ctx, newListRenderer(ctx.Context(), store, models, adminReg, meta, registration.Options), pageChrome, basePath, basePath+url.PathEscape(pk)+"/", instancePtr.Elem(), crumb{Label: i18n.T(ctx.Context(), "admin.button.delete", "Delete")})
			if err != nil {
				return err
			}
			return render(ctx, http.StatusOK, deleteTemplate, deletePageData{
				chrome:    rc,
				Singular:  displayNoun(meta, registration.Options),
				PK:        pk,
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
				return err
			}

			return ctx.Redirect(basePath)

		default:
			return methodNotAllowed(ctx)
		}
	}
}
