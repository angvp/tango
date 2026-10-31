package admin

import (
	"errors"
	"net/http"
	"reflect"
	"strconv"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

func createView(store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, registration ModelRegistration, nav []navItem, brand Branding) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"
	pageChrome := chrome{Nav: nav, Active: meta.Name, Brand: brand}

	return func(ctx *tango.Context) error {
		title := i18n.T(ctx.Context(), "admin.create.title", "New %s", displayNoun(meta, registration.Options))
		createChrome := pageChrome.withContext(ctx.Context()).withCrumbs(crumb{Label: sectionName(meta, registration.Options), Href: basePath}, crumb{Label: title})
		switch ctx.Request().Method {
		case http.MethodGet:
			fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, reflect.Value{}, formOptionsFromRequest(ctx))
			return render(ctx, http.StatusOK, formTemplate, formPageData{chrome: createChrome, Title: title, Fields: fields, CSRFToken: csrfTokenFromRequest(ctx.Request())})

		case http.MethodPost:
			if err := ctx.Request().ParseForm(); err != nil {
				return err
			}
			if !verifySessionCSRF(ctx) {
				return forbiddenCSRF(ctx)
			}

			instancePtr := reflect.New(meta.Type)
			if err := populateFromForm(ctx.Context(), store, models, adminReg, instancePtr.Elem(), meta, registration.Options, ctx.Request().PostForm, reflect.Value{}); err != nil {
				var refused *formFieldErrors
				if !errors.As(err, &refused) {
					return err
				}
				formOpts := formOptionsFromRequest(ctx)
				formOpts.FieldErrors = refused.messages
				formOpts.Submitted = ctx.Request().PostForm
				fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, instancePtr.Elem(), formOpts)
				return render(ctx, http.StatusUnprocessableEntity, formTemplate, formPageData{
					chrome:    createChrome,
					Title:     title,
					Fields:    fields,
					CSRFToken: csrfTokenFromRequest(ctx.Request()),
				})
			}

			if err := store.Create(ctx.Context(), meta, instancePtr.Interface()); err != nil {
				fieldErrors, formError, refused := storeFailure(ctx.Context(), err, meta, registration.Options)
				if !refused {
					return err
				}
				formOpts := formOptionsFromRequest(ctx)
				formOpts.FieldErrors = fieldErrors
				fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, instancePtr.Elem(), formOpts)
				return render(ctx, http.StatusUnprocessableEntity, formTemplate, formPageData{
					chrome:    createChrome,
					Title:     title,
					Error:     formError,
					Fields:    fields,
					CSRFToken: csrfTokenFromRequest(ctx.Request()),
				})
			}

			if err := setFlash(ctx.ResponseWriter(), ctx.Request(), sessionTokenOf(ctx.Request()), "admin.outcome.created", displayNoun(meta, registration.Options)); err != nil {
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
