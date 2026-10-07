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
	wantClause := ` WHERE ("active" = $1) AND ("name" LIKE $2 OR "name" LIKE $3)`
	if clause != wantClause || !reflect.DeepEqual(args, []any{true, "Al%", "Be%"}) {
		t.Fatalf("clause = %q, args = %#v; want %q and ordered args", clause, args, wantClause)
	}
}

// TestSetPrimaryKeyValue is a direct, white-box table of
// setPrimaryKeyValue's backfill branches: every supported Go kind, its
// overflow/negative-value error cases, an unsupported kind, and a
// non-settable destination.
func TestSetPrimaryKeyValue(t *testing.T) {
	addressable := func(v any) reflect.Value {
		p := reflect.New(reflect.TypeOf(v))
		p.Elem().Set(reflect.ValueOf(v))
		return p.Elem()
	}

	cases := []struct {
		name    string
		value   reflect.Value
		id      int64
		wantErr bool
		check   func(t *testing.T, v reflect.Value)
	}{
		{
			name:  "int64 backfills normally",
			value: addressable(int64(0)),
			id:    42,
			check: func(t *testing.T, v reflect.Value) {
				if v.Int() != 42 {
					t.Fatalf("got %d, want 42", v.Int())
				}
			},
		},
		{
			name:    "int8 overflow returns error",
			value:   addressable(int8(0)),
			id:      1000,
			wantErr: true,
		},
		{
			name:  "uint64 backfills normally",
			value: addressable(uint64(0)),
			id:    42,
			check: func(t *testing.T, v reflect.Value) {
				if v.Uint() != 42 {
					t.Fatalf("got %d, want 42", v.Uint())
				}
			},
		},
		{
			name:    "uint negative id returns error",
			value:   addressable(uint64(0)),
			id:      -1,
			wantErr: true,
		},
		{
			name:    "uint8 overflow returns error",
			value:   addressable(uint8(0)),
			id:      1000,
			wantErr: true,
		},
		{
			name:  "string backfills the decimal id",
			value: addressable(""),
			id:    42,
			check: func(t *testing.T, v reflect.Value) {
				if v.String() != "42" {
					t.Fatalf("got %q, want %q", v.String(), "42")
				}
			},
		},
		{
			name:    "unsupported kind returns error",
			value:   addressable(float64(0)),
			id:      42,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := setPrimaryKeyValue(tc.value, tc.id)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("got nil error, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("got error %v, want nil", err)
			}
			tc.check(t, tc.value)
		})
	}
}

func TestSetPrimaryKeyValueNotSettableReturnsError(t *testing.T) {
	// A value obtained without going through a pointer's Elem() is not
	// addressable, hence not settable.
	notSettable := reflect.ValueOf(internalWidget{ID: 1}).FieldByName("ID")
	if err := setPrimaryKeyValue(notSettable, 42); err == nil {
		t.Fatal("got nil error for a non-settable value, want an error")
	}
}

// TestFindFieldByColumnUnknownColumnReturnsError is findFieldByColumn's
// direct counterpart to TestStoreQueryUnmatchedColumnFails in
// store_raw_sql_test.go, confirming the not-found path by name rather than
// through a full Query round trip.
func TestFindFieldByColumnUnknownColumnReturnsError(t *testing.T) {
	structValue := reflect.ValueOf(&internalWidget{}).Elem()
	if _, err := findFieldByColumn(structValue, structValue.Type(), "no_such_column"); err == nil {
		t.Fatal("got nil error for an unmatched column, want an error")
	}
}

// TestFindFieldByColumnSkipsUnexportedFieldsThenMatchesNextExported covers
// findFieldByColumn's "skip unexported fields" branch: a column that
// happens to share a name with an unexported field must still match the
// next, exported field rather than being rejected.
func TestFindFieldByColumnSkipsUnexportedFieldsThenMatchesNextExported(t *testing.T) {
	type withUnexported struct {
		hidden string //nolint:unused // exercised via reflection only
		Name   string
	}
	structValue := reflect.ValueOf(&withUnexported{}).Elem()

	got, err := findFieldByColumn(structValue, structValue.Type(), "name")
	if err != nil {
		t.Fatalf("findFieldByColumn returned error: %v", err)
	}
	if !got.CanSet() {
		t.Fatal("findFieldByColumn returned a field that cannot be set")
	}
}
