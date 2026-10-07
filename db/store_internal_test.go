package db

import (
	"reflect"
	"testing"

	"github.com/angvp/tango/model"
)

// These tests reach unexported helpers directly and need no database.

type internalWidget struct {
	ID     int64 `tango:"pk"`
	Name   string
	Active bool
}

func internalWidgetMeta(t *testing.T) model.ModelMeta {
	t.Helper()
	registry := model.NewRegistry()
	if err := registry.Register(internalWidget{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	meta, _ := registry.Get("internalWidget")
	return meta
}

func TestStoreWhereClausePostgresPlaceholderOrder(t *testing.T) {
	meta := internalWidgetMeta(t)
	store := NewStore(nil, Postgres)
	clause, args, err := store.whereClause(meta, Query{
		Where: []Condition{{Field: "Active", Op: OpEq, Value: true}},
		Any: []Condition{
			{Field: "Name", Op: OpLike, Value: "Al%"},
			{Field: "Name", Op: OpLike, Value: "Be%"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantClause := " WHERE (active = $1) AND (name LIKE $2 OR name LIKE $3)"
	if clause != wantClause || !reflect.DeepEqual(args, []any{true, "Al%", "Be%"}) {
		t.Fatalf("clause = %q, args = %#v; want %q and ordered args", clause, args, wantClause)
	}
}

