package admin

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strconv"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

func listView(store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, registration ModelRegistration, nav []navItem, brand Branding) tango.View {
	meta := registration.Model
	basePath := "/admin/" + db.ColumnName(meta.Name) + "/"

	return func(ctx *tango.Context) error {
		if ctx.Request().Method != http.MethodGet {
			return methodNotAllowed(ctx)
		}

		page := parsePage(ctx.Query("page"))
		q := ctx.Query("q")
		query := db.Query{OrderBy: registration.Options.Ordering}
		if q != "" {
			for _, field := range registration.Options.Search {
				query.Any = append(query.Any, db.Condition{Field: field, Op: db.OpLike, Value: "%" + q + "%"})
			}
		}
		filters, err := resolveListFilters(ctx.Context(), store, models, adminReg, meta, registration.Options, ctx.Query)
		if err != nil {
			return err
		}
		query.Where = filters.Where

		// Count first so an out-of-range page is clamped to the last page
		// before any rows are fetched; rows and pager then describe the
		// same page.
		total, err := store.Count(ctx.Context(), meta, query)
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
		// Fetch one extra row beyond the page size so HasNext reflects
		// whether a next page actually has rows, rather than assuming one
		// exists whenever this page happens to be exactly full.
		query.Limit = pageSize + 1
		query.Offset = (page - 1) * pageSize

		sliceType := reflect.SliceOf(meta.Type)
		destPtr := reflect.New(sliceType)

		if err := store.List(ctx.Context(), meta, query, destPtr.Interface()); err != nil {
			return err
		}

		rows, err := buildRows(ctx.Context(), store, models, adminReg, destPtr.Elem(), meta, registration.Options)
		if err != nil {
			return err
		}

		hasNext := len(rows) > pageSize
		if hasNext {
			rows = rows[:pageSize]
		}

		params := listParams(q, filters.Params)

		columns := make([]string, len(registration.Options.ListDisplay))
		for i, name := range registration.Options.ListDisplay {
			columns[i] = listHeader(name, registration.Options)
		}

		data := listPageData{
			chrome:      chrome{Nav: nav, Active: meta.Name, Brand: brand}.withContext(ctx.Context()),
			ModelName:   meta.Name,
			BasePath:    basePath,
			Columns:     columns,
			Rows:        rows,
			SearchQuery: q,
			Filters:     filters.Controls,
			Criteria:    hasCriteria(q, filters),
			Page:        page,
			TotalPages:  totalPages,
			TotalCount:  total,
			PageLinks:   withPageHrefs(paginationLinks(page, totalPages), params),
			HasPrev:     page > 1,
			PrevHref:    pageHref(params, page-1),
			HasNext:     hasNext,
			NextHref:    pageHref(params, page+1),
		}

		return render(ctx, http.StatusOK, listTemplate, data)
	}
}

func buildRows(ctx context.Context, store *db.Store, models *model.Registry, adminReg *adminregistry.Registry, sliceValue reflect.Value, meta model.ModelMeta, opts adminregistry.Options) ([]listRow, error) {
	pkField, err := primaryKeyField(meta)
	if err != nil {
		return nil, err
	}

	cells := listRenderer{ctx: ctx, store: store, models: models, adminReg: adminReg, meta: meta, display: opts.Display}
	rows := make([]listRow, 0, sliceValue.Len())
	for i := 0; i < sliceValue.Len(); i++ {
		elem := sliceValue.Index(i)
		row := listRow{
			PK:    fmt.Sprint(elem.FieldByName(pkField.Name).Interface()),
			Cells: make([]listCell, len(opts.ListDisplay)),
		}
		for j, column := range opts.ListDisplay {
			row.Cells[j] = cells.cell(elem, column)
		}
		rows = append(rows, row)
	}

	return rows, nil
}

func parsePage(raw string) int {
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 1
	}
	return page
}

// pageLink is one entry in a rendered pagination control: either a page
// number (selectable, possibly the current page) or an ellipsis marker.
type pageLink struct {
	Number   int
	Current  bool
	Ellipsis bool
	Href     string
}

// Pagination truncation constants: three page links on each side of the
// current page and two at each end, producing a
// "1 2 … 7 8 9 10 11 12 13 … 19 20" shape for long page ranges.
const (
	paginationOnEachSide = 3
	paginationOnEnds     = 2
)

// paginationLinks builds the truncated page list for a
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
