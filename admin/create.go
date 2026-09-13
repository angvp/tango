package admin

import (
	"net/http"
	"reflect"
	"strconv"

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
			fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, reflect.Value{}, formOptionsFromRequest(ctx))
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
				fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, instancePtr.Elem(), formOptionsFromRequest(ctx))
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

			target := basePath
			if next := ctx.Query("next"); next != "" {
				target = safeAdminNext(next, basePath)
				if target != basePath {
					target = addCreatedObjectPreselect(ctx, target, meta, instancePtr.Elem())
				}
			}
			return ctx.Redirect(target)

		default:
			return methodNotAllowed(ctx)
		}
	}
}

func formOptionsFromRequest(ctx *tango.Context) formContextOptions {
	return formContextOptions{
		CurrentURL:     ctx.Request().URL.RequestURI(),
		PreselectField: ctx.Query(adminPreselectFieldParam),
		PreselectValue: ctx.Query(adminPreselectValueParam),
	}
}

func addCreatedObjectPreselect(ctx *tango.Context, target string, meta model.ModelMeta, instance reflect.Value) string {
	fieldName := ctx.Query(adminPreselectFieldParam)
	if fieldName == "" {
		return target
	}
	pkField, err := primaryKeyField(meta)
	if err != nil {
		return target
	}
	pkValue := instance.FieldByName(pkField.Name)
	return appendQuery(target, adminPreselectValueParam, formatPreselectPK(pkValue))
}

func formatPreselectPK(value reflect.Value) string {
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(value.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(value.Uint(), 10)
	default:
		return formatFieldValue(value)
	}
}
