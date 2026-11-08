package db_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
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

// TestZeroDateIsRejectedAsAQueryValueOnEveryOperator is the Date half of the
// rejection matrix: a zero model.Date fails before any SQL, on every dialect,
// for every value-taking operator and in every query shape.
func TestZeroDateIsRejectedAsAQueryValueOnEveryOperator(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres, db.MySQL} {
		for _, op := range zeroValueOperators {
			for _, shape := range zeroDateQueryShapes(op) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), dialect)
				var rows []dateEvent
				err := store.List(context.Background(), metaFor(t, registerModels(t, dateEvent{}), dateEvent{}), shape.query, &rows)
				if !errors.Is(err, db.ErrInvalidQuery) || !strings.Contains(errString(err), "IsNull") {
					t.Errorf("dialect %d, %s, %s: err = %v, want ErrInvalidQuery naming IsNull", dialect, op, shape.name, err)
				}
				if n := len(rec.statements()); n != 0 {
					t.Errorf("dialect %d, %s, %s: %d statements issued, want none", dialect, op, shape.name, n)
				}
			}
		}
	}
}

func zeroDateQueryShapes(op db.Op) []zeroValueQueryShape {
	condition := db.Condition{Field: "Day", Op: op, Value: model.Date{}}
	return []zeroValueQueryShape{
		{name: "Where", query: db.Query{Where: []db.Condition{condition}}},
		{name: "Any", query: db.Query{Any: []db.Condition{condition}}},
		{name: "Filter", query: db.Query{Filter: ptr(db.Cond("Day", op, model.Date{}))}},
	}
}

// TestZeroDateInInIsRejected covers the IN element, which the operator matrix
// above does not reach.
func TestZeroDateInInIsRejected(t *testing.T) {
	rec := &recorder{}
	store := db.NewStore(sql.OpenDB(rec), db.SQLite)
	var rows []dateEvent
	err := store.List(context.Background(), metaFor(t, registerModels(t, dateEvent{}), dateEvent{}), db.Query{Filter: ptr(db.In("Day", []model.Date{{}}))}, &rows)
	if !errors.Is(err, db.ErrInvalidQuery) || len(rec.statements()) != 0 {
		t.Errorf("zero Date in IN: err = %v, statements = %d; want ErrInvalidQuery and no SQL", err, len(rec.statements()))
	}
}

// locatedZeroTime is the zero instant in a named zone. It is IsZero() by
// time.Time's own rule but not by reflect's, so a check that relies on
// reflect.Value.IsZero misses it.
var locatedZeroTime = time.Time{}.In(time.FixedZone("Z0", 0))

// TestLocatedZeroTimeIsRejectedOnEveryOperatorAndIN: a zero instant that
// carries a location is still the zero time, and is rejected as a query value
// by every value operator and in every IN element.
func TestLocatedZeroTimeIsRejectedOnEveryOperatorAndIN(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres, db.MySQL} {
		for _, op := range zeroValueOperators {
			for _, shape := range zeroValueQueryShapes(op, locatedZeroTime) {
				rec := &recorder{}
				store := db.NewStore(sql.OpenDB(rec), dialect)
				var rows []goldenPost
				err := store.List(context.Background(), registerGoldenModels(t), shape.query, &rows)
				if !errors.Is(err, db.ErrInvalidQuery) || len(rec.statements()) != 0 {
					t.Errorf("dialect %d, %s, %s: err = %v, statements = %d; want ErrInvalidQuery and no SQL", dialect, op, shape.name, err, len(rec.statements()))
				}
			}
		}
		rec := &recorder{}
		store := db.NewStore(sql.OpenDB(rec), dialect)
		var rows []goldenPost
		err := store.List(context.Background(), registerGoldenModels(t), db.Query{Filter: ptr(db.In("CreatedAt", []time.Time{locatedZeroTime}))}, &rows)
		if !errors.Is(err, db.ErrInvalidQuery) || len(rec.statements()) != 0 {
			t.Errorf("dialect %d, IN: err = %v, statements = %d; want ErrInvalidQuery and no SQL", dialect, err, len(rec.statements()))
		}
	}
}
