package admin

import (
	"net/http"
	"reflect"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

func createView(store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, registration ModelRegistration, nav []navItem, brand Branding) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"
	title := "New " + meta.Name
	pageChrome := chrome{Nav: nav, Active: meta.Name, Brand: brand}

	return func(ctx *tango.Context) error {
		switch ctx.Request().Method {
		case http.MethodGet:
			fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, reflect.Value{})
			return render(ctx, http.StatusOK, formTemplate, formPageData{chrome: pageChrome, Title: title, Fields: fields, CSRFToken: csrfTokenFromRequest(ctx.Request())})

		case http.MethodPost:
			if err := ctx.Request().ParseForm(); err != nil {
				return err
			}
			if !verifySessionCSRF(ctx) {
				return forbiddenCSRF(ctx)
			}

			instancePtr := reflect.New(meta.Type)
			if err := populateFromForm(ctx.Context(), store, models, adminReg, instancePtr.Elem(), meta, registration.Options, ctx.Request().PostForm, reflect.Value{}); err != nil {
				fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, instancePtr.Elem())
				return render(ctx, http.StatusUnprocessableEntity, formTemplate, formPageData{
					chrome:    pageChrome,
					Title:     title,
					Error:     err.Error(),
					Fields:    fields,
					CSRFToken: csrfTokenFromRequest(ctx.Request()),
				})
			}

			if err := store.Create(ctx.Context(), meta, instancePtr.Interface()); err != nil {
				return err
			}

			return ctx.Redirect(basePath)

		default:
			return methodNotAllowed(ctx)
		}
	}
}
