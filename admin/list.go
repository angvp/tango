package admin

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

func listView(store *db.Store, registration ModelRegistration) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"

	return func(ctx *tango.Context) error {
		if ctx.Request().Method != http.MethodGet {
			return methodNotAllowed(ctx)
		}

		page := parsePage(ctx.Query("page"))
		q := ctx.Query("q")
		query := db.Query{Limit: pageSize, Offset: (page - 1) * pageSize}

		sliceType := reflect.SliceOf(meta.Type)
		destPtr := reflect.New(sliceType)

		var err error
		if q != "" && len(registration.Options.Search) > 0 {
			err = searchList(ctx.Context(), store, meta, registration.Options.Search, q, query, destPtr.Interface())
		} else {
			err = store.List(ctx.Context(), meta, query, destPtr.Interface())
		}
		if err != nil {
			return err
		}

		rows, err := buildRows(destPtr.Elem(), meta, registration.Options.ListDisplay)
		if err != nil {
			return err
		}

		data := listPageData{
			ModelName:   meta.Name,
			BasePath:    basePath,
			Columns:     registration.Options.ListDisplay,
			Rows:        rows,
			SearchQuery: q,
			HasPrev:     page > 1,
			PrevPage:    page - 1,
			HasNext:     len(rows) == pageSize,
			NextPage:    page + 1,
		}

		return render(ctx, http.StatusOK, listTemplate, data)
	}
}

func buildRows(sliceValue reflect.Value, meta model.ModelMeta, columns []string) ([]listRow, error) {
	pkField, err := primaryKeyField(meta)
	if err != nil {
		return nil, err
	}

	rows := make([]listRow, 0, sliceValue.Len())
	for i := 0; i < sliceValue.Len(); i++ {
		elem := sliceValue.Index(i)
		row := listRow{
			PK:     fmt.Sprint(elem.FieldByName(pkField.Name).Interface()),
			Values: make([]string, len(columns)),
		}
		for j, column := range columns {
			row.Values[j] = fmt.Sprint(elem.FieldByName(column).Interface())
		}
		rows = append(rows, row)
	}

	return rows, nil
}

func searchList(ctx context.Context, store *db.Store, meta model.ModelMeta, searchFields []string, q string, query db.Query, dest any) error {
	selectColumns := make([]string, len(meta.Fields))
	for i, field := range meta.Fields {
		selectColumns[i] = db.ColumnName(field.Name) + " AS " + field.Name
	}

	var likeClauses []string
	var args []any
	for _, name := range searchFields {
		likeClauses = append(likeClauses, db.ColumnName(name)+" LIKE ?")
		args = append(args, "%"+q+"%")
	}

	sqlQuery := fmt.Sprintf("SELECT %s FROM %s", strings.Join(selectColumns, ", "), db.ColumnName(meta.Name))
	if len(likeClauses) > 0 {
		sqlQuery += " WHERE " + strings.Join(likeClauses, " OR ")
	}
	sqlQuery += fmt.Sprintf(" LIMIT %d OFFSET %d", query.Limit, query.Offset)

	return store.Query(ctx, dest, sqlQuery, args...)
}

func parsePage(raw string) int {
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 1
	}
	return page
}
