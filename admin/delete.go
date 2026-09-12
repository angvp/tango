package admin

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
)

func deleteView(store *db.Store, registration ModelRegistration, nav []navItem) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"
	pageChrome := chrome{Nav: nav, Active: meta.Name}

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

			return render(ctx, http.StatusOK, deleteTemplate, deletePageData{
				chrome:    pageChrome,
				ModelName: meta.Name,
				PK:        fmt.Sprint(pkValue),
			})

		case http.MethodPost:
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
