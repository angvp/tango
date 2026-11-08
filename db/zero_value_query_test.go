package db_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/db"
)

// zeroValueOperators are the value-taking operators a zero time must not be
// bound to. A zero value is not a query value: binding it would match
// nothing, silently, on some dialects and not others.
var zeroValueOperators = []db.Op{db.OpEq, db.OpNe, db.OpGt, db.OpGte, db.OpLt, db.OpLte}

// TestZeroTimeIsRejectedAsAQueryValueOnEveryOperator pins down that a zero
// time.Time fails validation before any SQL is issued, on every dialect and
// for every value-taking operator, in Where, in Any and in a Filter leaf.
func TestZeroTimeIsRejectedAsAQueryValueOnEveryOperator(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres, db.MySQL} {
		for _, op := range zeroValueOperators {
			for _, shape := range zeroValueQueryShapes(op, time.Time{}) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), dialect)
				var rows []goldenPost
				err := store.List(context.Background(), registerGoldenModels(t), shape.query, &rows)
				if !errors.Is(err, db.ErrInvalidQuery) {
					t.Errorf("dialect %d, %s, %s: err = %v, want ErrInvalidQuery", dialect, op, shape.name, err)
				}
				if !strings.Contains(errString(err), "IsNull") {
					t.Errorf("dialect %d, %s, %s: message %q does not point at IsNull", dialect, op, shape.name, errString(err))
				}
				if n := len(rec.statements()); n != 0 {
					t.Errorf("dialect %d, %s, %s: %d statements issued, want none", dialect, op, shape.name, n)
				}
			}
		}
	}
}

// zeroValueQueryShape is one place a condition can sit in a Query.
type zeroValueQueryShape struct {
	name  string
	query db.Query
}

func zeroValueQueryShapes(op db.Op, value time.Time) []zeroValueQueryShape {
	condition := db.Condition{Field: "CreatedAt", Op: op, Value: value}
	return []zeroValueQueryShape{
		{name: "Where", query: db.Query{Where: []db.Condition{condition}}},
		{name: "Any", query: db.Query{Any: []db.Condition{condition}}},
		{name: "Filter", query: db.Query{Filter: ptr(db.Cond("CreatedAt", op, value))}},
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
