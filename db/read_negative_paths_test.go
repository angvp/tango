package db_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

func TestFirstAndExistsFailOnAMissingTable(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	meta := registerModel(t, createWidget{})
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()

	var dest createWidget
	err := store.First(ctx, meta, db.Query{}, &dest)
	if err == nil {
		t.Fatal("First on a table that was never created succeeded")
	}
	if errors.Is(err, db.ErrNotFound) || errors.Is(err, db.ErrInvalidQuery) {
		t.Errorf("First error = %v, want the driver's error, not a not-found or invalid-query sentinel", err)
	}

	found, err := store.Exists(ctx, meta, db.Query{})
	if err == nil {
		t.Fatal("Exists on a table that was never created succeeded")
	}
	if found {
		t.Errorf("Exists reported a match alongside its error")
	}
}

func TestFirstAndExistsRefuseACancelledContext(t *testing.T) {
	store, _ := seedWidgets(t)
	meta := registerModel(t, createWidget{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var dest createWidget
	if err := store.First(ctx, meta, db.Query{}, &dest); !errors.Is(err, context.Canceled) {
		t.Errorf("First error = %v, want context.Canceled", err)
	}
	if dest != (createWidget{}) {
		t.Errorf("dest changed by a cancelled First: %+v", dest)
	}
	if found, err := store.Exists(ctx, meta, db.Query{}); !errors.Is(err, context.Canceled) || found {
		t.Errorf("Exists = %v, %v, want false and context.Canceled", found, err)
	}
}

func TestFirstRefusesAModelWithoutAPrimaryKeyBeforeAnySQL(t *testing.T) {
	meta := model.ModelMeta{
		Name:   "keyless",
		Type:   reflect.TypeOf(createWidget{}),
		Fields: []model.FieldMeta{{Name: "Name", Type: reflect.TypeOf("")}},
	}
	rec := &recorder{}
	store := db.NewStore(sql.OpenDB(rec), db.SQLite)

	var dest createWidget
	err := store.First(context.Background(), meta, db.Query{}, &dest)
	if want := "tango db: model keyless has no primary key field"; err == nil || err.Error() != want {
		t.Fatalf("First error = %v, want %q", err, want)
	}
	if n := len(rec.statements()); n != 0 {
		t.Errorf("First sent %d statements for a model with no primary key, want 0", n)
	}
}

func TestFilterGroupRefusesAnInvalidChildBeforeAnySQL(t *testing.T) {
	meta := registerModel(t, createWidget{})
	rec := &recorder{}
	store := db.NewStore(sql.OpenDB(rec), db.SQLite)
	query := db.Query{Filter: ptr(db.And(db.Cond("Name", db.OpEq, "a"), db.Cond("Nope", db.OpEq, 1)))}

	var rows []createWidget
	err := store.List(context.Background(), meta, query, &rows)
	want := `tango db: invalid query: unknown field "Nope" for model createWidget`
	if err == nil || err.Error() != want {
		t.Fatalf("List error = %v, want %q", err, want)
	}
	if !errors.Is(err, db.ErrInvalidQuery) {
		t.Errorf("errors.Is(err, ErrInvalidQuery) = false for %v", err)
	}
	if n := len(rec.statements()); n != 0 {
		t.Errorf("a refused group sent %d statements, want 0", n)
	}
}

func TestQueryLimitErrorNamesAnUnlistedLimit(t *testing.T) {
	err := &db.QueryLimitError{Limit: "widgets", Max: 1, Got: 2}
	if want := "tango db: invalid query: widgets is 2, limit 1"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, db.ErrQueryLimit) || !errors.Is(err, db.ErrInvalidQuery) {
		t.Errorf("errors.Is must match ErrQueryLimit and ErrInvalidQuery for %v", err)
	}
}

func TestInvalidJSONErrorUnwrapsToTheSyntaxProblem(t *testing.T) {
	store, meta, _ := openJSONNotes(t)

	err := store.Create(context.Background(), meta, &JSONNote{Body: `{"a":`})
	var jsonErr *db.InvalidJSONError
	if !errors.As(err, &jsonErr) {
		t.Fatalf("Create error = %v, want an *InvalidJSONError", err)
	}
	if jsonErr.Err == nil {
		t.Fatal("InvalidJSONError.Err is nil, want the syntax problem")
	}
	if got := errors.Unwrap(jsonErr); got != jsonErr.Err {
		t.Errorf("Unwrap() = %v, want the Err field %v", got, jsonErr.Err)
	}
	if strings.Contains(jsonErr.Err.Error(), `{"a":`) {
		t.Errorf("syntax problem %q echoes the submitted text", jsonErr.Err)
	}
}
