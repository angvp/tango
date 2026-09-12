package admin

import (
	"errors"
	"net/http"
	"reflect"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
)

func editView(store *db.Store, registration ModelRegistration) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"
	title := "Edit " + meta.Name

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

			fields := buildFormFields(meta, instancePtr.Elem())
			return render(ctx, http.StatusOK, formTemplate, formPageData{Title: title, Fields: fields})

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

			instancePtr := reflect.New(meta.Type)
			if err := populateFromForm(instancePtr.Elem(), meta, ctx.Request().PostForm); err != nil {
				fields := buildFormFields(meta, instancePtr.Elem())
				return render(ctx, http.StatusUnprocessableEntity, formTemplate, formPageData{
					Title:  title,
					Error:  err.Error(),
					Fields: fields,
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
