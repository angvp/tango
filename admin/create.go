package admin

import (
	"net/http"
	"reflect"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
)

func createView(store *db.Store, registration ModelRegistration, nav []navItem) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"
	title := "New " + meta.Name
	pageChrome := chrome{Nav: nav, Active: meta.Name}

	return func(ctx *tango.Context) error {
		switch ctx.Request().Method {
		case http.MethodGet:
			fields := buildFormFields(meta, reflect.Value{})
			return render(ctx, http.StatusOK, formTemplate, formPageData{chrome: pageChrome, Title: title, Fields: fields})

		case http.MethodPost:
			if err := ctx.Request().ParseForm(); err != nil {
				return err
			}

			instancePtr := reflect.New(meta.Type)
			if err := populateFromForm(instancePtr.Elem(), meta, ctx.Request().PostForm); err != nil {
				fields := buildFormFields(meta, instancePtr.Elem())
				return render(ctx, http.StatusUnprocessableEntity, formTemplate, formPageData{
					chrome: pageChrome,
					Title:  title,
					Error:  err.Error(),
					Fields: fields,
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
