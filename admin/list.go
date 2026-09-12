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

func listView(store *db.Store, registration ModelRegistration, nav []navItem) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"

	return func(ctx *tango.Context) error {
		if ctx.Request().Method != http.MethodGet {
			return methodNotAllowed(ctx)
		}

		page := parsePage(ctx.Query("page"))
		q := ctx.Query("q")
		// Fetch one extra row beyond the page size so HasNext reflects
		// whether a next page actually has rows, rather than assuming one
		// exists whenever this page happens to be exactly full.
		query := db.Query{Limit: pageSize + 1, Offset: (page - 1) * pageSize}

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

		hasNext := len(rows) > pageSize
		if hasNext {
			rows = rows[:pageSize]
		}

		total, err := countRows(ctx.Context(), store, meta, registration.Options.Search, q)
		if err != nil {
			return err
		}
		totalPages := (total + pageSize - 1) / pageSize
		if totalPages < 1 {
			totalPages = 1
		}
		if page > totalPages {
			page = totalPages
		}

		columns := make([]string, len(registration.Options.ListDisplay))
		for i, name := range registration.Options.ListDisplay {
			columns[i] = humanizeFieldName(name)
		}

		data := listPageData{
			chrome:      chrome{Nav: nav, Active: meta.Name},
			ModelName:   meta.Name,
			BasePath:    basePath,
			Columns:     columns,
			Rows:        rows,
			SearchQuery: q,
			Page:        page,
			TotalPages:  totalPages,
			TotalCount:  total,
			PageLinks:   paginationLinks(page, totalPages),
			HasPrev:     page > 1,
			PrevPage:    page - 1,
			HasNext:     hasNext,
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
			row.Values[j] = formatFieldValue(elem.FieldByName(column))
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

// countRows returns the total number of rows matching the same filter
// searchList/store.List would apply, so the paginator can show a true page
// count instead of guessing from whether the current page happens to be full.
func countRows(ctx context.Context, store *db.Store, meta model.ModelMeta, searchFields []string, q string) (int, error) {
	var args []any
	sqlQuery := "SELECT COUNT(*) AS Count FROM " + db.ColumnName(meta.Name)
	if q != "" && len(searchFields) > 0 {
		likeClauses := make([]string, len(searchFields))
		for i, name := range searchFields {
			likeClauses[i] = db.ColumnName(name) + " LIKE ?"
			args = append(args, "%"+q+"%")
		}
		sqlQuery += " WHERE " + strings.Join(likeClauses, " OR ")
	}

	var result struct{ Count int }
	if err := store.QueryRow(ctx, &result, sqlQuery, args...); err != nil {
		return 0, err
	}
	return result.Count, nil
}

// pageLink is one entry in a rendered pagination control: either a page
// number (selectable, possibly the current page) or an ellipsis marker.
type pageLink struct {
	Number   int
	Current  bool
	Ellipsis bool
}

// Django admin's own pagination truncation constants (see
// django.contrib.admin.views.main.ChangeList: ALL_VAR pagination), producing
// the same "1 2 … 7 8 9 10 11 12 13 … 19 20" shape for long page ranges.
const (
	paginationOnEachSide = 3
	paginationOnEnds     = 2
)

// paginationLinks builds the Django-admin-style truncated page list for a
// paginator with totalPages pages, currently on page current.
func paginationLinks(current, totalPages int) []pageLink {
	if totalPages <= 1 {
		return nil
	}

	if totalPages <= (paginationOnEachSide+paginationOnEnds)*2 {
		links := make([]pageLink, totalPages)
		for i := range links {
			links[i] = pageLink{Number: i + 1, Current: i+1 == current}
		}
		return links
	}

	var numbers []int
	appendRange := func(from, to int) {
		for i := from; i <= to; i++ {
			numbers = append(numbers, i)
		}
	}

	if current > (1+paginationOnEachSide+paginationOnEnds)+1 {
		appendRange(1, paginationOnEnds)
		numbers = append(numbers, 0)
		appendRange(current-paginationOnEachSide, current)
	} else {
		appendRange(1, current)
	}

	if current < (totalPages-paginationOnEachSide-paginationOnEnds)-1 {
		appendRange(current+1, current+paginationOnEachSide)
		numbers = append(numbers, 0)
		appendRange(totalPages-paginationOnEnds+1, totalPages)
	} else {
		appendRange(current+1, totalPages)
	}

	links := make([]pageLink, len(numbers))
	for i, n := range numbers {
		if n == 0 {
			links[i] = pageLink{Ellipsis: true}
			continue
		}
		links[i] = pageLink{Number: n, Current: n == current}
	}
	return links
}
