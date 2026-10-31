package admin

import (
	"errors"
	"net/http"
	"reflect"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

func editView(store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, registration ModelRegistration, nav []navItem, brand Branding) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"
	pageChrome := chrome{Nav: nav, Active: meta.Name, Brand: brand}

	return func(ctx *tango.Context) error {
		title := i18n.T(ctx.Context(), "admin.edit.title", "Edit %s", displayNoun(meta, registration.Options))
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

			rc, err := rowChrome(ctx, newListRenderer(ctx.Context(), store, models, adminReg, meta, registration.Options), pageChrome, basePath, "", instancePtr.Elem())
			if err != nil {
				return err
			}
			fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, instancePtr.Elem(), formOptionsFromRequest(ctx))
			return render(ctx, http.StatusOK, formTemplate, formPageData{chrome: rc, Title: title, Fields: fields, CSRFToken: csrfTokenFromRequest(ctx.Request())})

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

			rc, err := rowChrome(ctx, newListRenderer(ctx.Context(), store, models, adminReg, meta, registration.Options), pageChrome, basePath, "", existingPtr.Elem())
			if err != nil {
				return err
			}
			instancePtr := reflect.New(meta.Type)
			if err := populateFromForm(ctx.Context(), store, models, adminReg, instancePtr.Elem(), meta, registration.Options, ctx.Request().PostForm, existingPtr.Elem()); err != nil {
				var refused *formFieldErrors
				if !errors.As(err, &refused) {
					return err
				}
				formOpts := formOptionsFromRequest(ctx)
				formOpts.FieldErrors = refused.messages
				formOpts.Submitted = ctx.Request().PostForm
				fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, instancePtr.Elem(), formOpts)
				return render(ctx, http.StatusUnprocessableEntity, formTemplate, formPageData{
					chrome:    rc,
					Title:     title,
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
				fieldErrors, formError, refused := storeFailure(ctx.Context(), err, meta, registration.Options)
				if !refused {
					return err
				}
				formOpts := formOptionsFromRequest(ctx)
				formOpts.FieldErrors = fieldErrors
				fields := buildFormFields(ctx.Context(), store, models, adminReg, meta, registration.Options, instancePtr.Elem(), formOpts)
				return render(ctx, http.StatusUnprocessableEntity, formTemplate, formPageData{
					chrome:    rc,
					Title:     title,
					Error:     formError,
					Fields:    fields,
					CSRFToken: csrfTokenFromRequest(ctx.Request()),
				})
			}

			return ctx.Redirect(basePath)

		default:
			return methodNotAllowed(ctx)
		}
	}
}

// rowChrome is the page chrome for a row's pages: the model, the row's label
// and then any tail steps. The label is a link to rowHref unless that is
// empty, which makes it the current step when no tail follows.
func rowChrome(ctx *tango.Context, cells listRenderer, base chrome, basePath, rowHref string, row reflect.Value, tail ...crumb) (chrome, error) {
	label, err := cells.identityOf(row)
	if err != nil {
		return chrome{}, err
	}
	trail := append([]crumb{{Label: cells.meta.Name, Href: basePath}, {Label: label, Href: rowHref}}, tail...)
	return base.withContext(ctx.Context()).withCrumbs(trail...), nil
}
