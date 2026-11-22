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

func createView(store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, registration ModelRegistration, nav []navItem, brand Branding, files *fileFields) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"
	pageChrome := chrome{Nav: nav, Active: meta.Name, Brand: brand}

	return func(ctx *tango.Context) error {
		title := i18n.T(ctx.Context(), "admin.create.title", "New %s", displayNoun(meta, registration.Options))
		createChrome := pageChrome.withContext(ctx.Context()).withCrumbs(crumb{Label: sectionName(meta, registration.Options), Href: basePath}, crumb{Label: title})
		switch ctx.Request().Method {
		case http.MethodGet:
			fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, reflect.Value{}, formOptionsFromRequest(ctx, files))
			return render(ctx, http.StatusOK, formTemplate, formPage(ctx.Context(), createChrome, title, meta, registration.Options, fields, "", csrfTokenFromRequest(ctx.Request())))

		case http.MethodPost:
			sub, err := readSubmission(ctx, files)
			if err != nil {
				if errors.Is(err, errSubmissionCSRF) {
					return forbiddenCSRF(ctx)
				}
				fieldErrors, formError, handled := sub.problem(ctx.Context(), err)
				if !handled {
					return err
				}
				formOpts := formOptionsFromRequest(ctx, files)
				formOpts.FieldErrors = fieldErrors
				formOpts.Submitted = sub.values
				fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, reflect.Value{}, formOpts)
				return render(ctx, http.StatusUnprocessableEntity, formTemplate, formPage(ctx.Context(), createChrome, title, meta, registration.Options, fields, formError, csrfTokenFromRequest(ctx.Request())))
			}

			instancePtr := reflect.New(meta.Type)
			if err := populateFromForm(ctx.Context(), store, models, adminReg, instancePtr.Elem(), meta, registration.Options, sub.form, reflect.Value{}); err != nil {
				sub.discard(ctx)
				var refused *formFieldErrors
				if !errors.As(err, &refused) {
					return err
				}
				formOpts := formOptionsFromRequest(ctx, files)
				formOpts.FieldErrors = refused.messages
				formOpts.Submitted = sub.values
				fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, instancePtr.Elem(), formOpts)
				return render(ctx, http.StatusUnprocessableEntity, formTemplate, formPage(ctx.Context(), createChrome, title, meta, registration.Options, fields, "", csrfTokenFromRequest(ctx.Request())))
			}

			if err := store.Create(ctx.Context(), meta, instancePtr.Interface()); err != nil {
				sub.discard(ctx)
				fieldErrors, formError, refused := storeFailure(ctx.Context(), err, meta, registration.Options)
				if !refused {
					return err
				}
				formOpts := formOptionsFromRequest(ctx, files)
				formOpts.FieldErrors = fieldErrors
				fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, instancePtr.Elem(), formOpts)
				return render(ctx, http.StatusUnprocessableEntity, formTemplate, formPage(ctx.Context(), createChrome, title, meta, registration.Options, fields, formError, csrfTokenFromRequest(ctx.Request())))
			}

			setOutcome(ctx, "admin.outcome.created", displayNoun(meta, registration.Options))
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

func formOptionsFromRequest(ctx *tango.Context, files *fileFields) formContextOptions {
	return formContextOptions{
		Files:          files,
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
