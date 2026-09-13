package admin

import (
	"errors"
	"net/http"
	"reflect"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

func editView(store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, registration ModelRegistration, nav []navItem, brand Branding) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"
	title := "Edit " + meta.Name
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

			fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, instancePtr.Elem(), formOptionsFromRequest(ctx))
			return render(ctx, http.StatusOK, formTemplate, formPageData{chrome: pageChrome, Title: title, Fields: fields, CSRFToken: csrfTokenFromRequest(ctx.Request())})

		case http.MethodPost:
			existingPtr := reflect.New(meta.Type)
			if err := store.Get(ctx.Context(), meta, pkValue, existingPtr.Interface()); err != nil {
				if errors.Is(err, db.ErrNotFound) {
					return notFound(ctx)
				}
				return err
			}

			if err := ctx.Request().ParseForm(); err != nil {
				return err
			}
			if !verifySessionCSRF(ctx) {
				return forbiddenCSRF(ctx)
			}

			instancePtr := reflect.New(meta.Type)
			if err := populateFromForm(ctx.Context(), store, models, adminReg, instancePtr.Elem(), meta, registration.Options, ctx.Request().PostForm, existingPtr.Elem()); err != nil {
				fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, instancePtr.Elem(), formOptionsFromRequest(ctx))
				return render(ctx, http.StatusUnprocessableEntity, formTemplate, formPageData{
					chrome:    pageChrome,
					Title:     title,
					Error:     err.Error(),
					Fields:    fields,
					CSRFToken: csrfTokenFromRequest(ctx.Request()),
				})
			}

			if err := setPKField(instancePtr.Elem().FieldByName(pkField.Name), pkField, ctx.Param("pk")); err != nil {
				return err
			}

			if err := store.Update(ctx.Context(), meta, instancePtr.Interface()); err != nil {
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
