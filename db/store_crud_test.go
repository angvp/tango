package db

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/angvp/tango/model"
	_ "modernc.org/sqlite"
)

type crudWidget struct {
	ID        int64 `tango:"pk"`
	Name      string
	Active    bool
	Count     int
	Score     float64
	CreatedAt time.Time
}

func TestStoreGetScansMatchingRowIntoDest(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	createdAt := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	widget := crudWidget{Name: "alpha", Active: true, Count: 3, Score: 1.5, CreatedAt: createdAt}
	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	var got crudWidget
	got.ID = widget.ID
	if err := store.Get(context.Background(), meta, widget.ID, &got); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}

	if got.Name != widget.Name || got.Active != widget.Active || got.Count != widget.Count || got.Score != widget.Score {
		t.Fatalf("Get scanned %+v, want fields matching %+v", got, widget)
	}
	if !got.CreatedAt.Equal(widget.CreatedAt) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, widget.CreatedAt)
	}
}

func TestStoreGetUnknownPrimaryKeyReturnsErrNotFound(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	var got crudWidget
	err := store.Get(context.Background(), meta, int64(999), &got)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want it to wrap ErrNotFound", err)
	}
	if got.Name != "" {
		t.Fatalf("dest was mutated on ErrNotFound: %+v", got)
	}
}

func TestStoreGetRoundTripsAllSupportedFieldKinds(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	widget := crudWidget{Name: "beta", Active: false, Count: -4, Score: 2.25, CreatedAt: createdAt}
	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	var got crudWidget
	if err := store.Get(context.Background(), meta, widget.ID, &got); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got != widget {
		if got.ID != widget.ID || got.Name != widget.Name || got.Active != widget.Active ||
			got.Count != widget.Count || got.Score != widget.Score || !got.CreatedAt.Equal(widget.CreatedAt) {
			t.Fatalf("Get = %+v, want %+v", got, widget)
		}
	}
}

func TestStoreUpdatePersistsFieldChanges(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	widget := crudWidget{Name: "gamma", Count: 1}
	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	widget.Name = "gamma-updated"
	widget.Count = 42
	if err := store.Update(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	var got crudWidget
	if err := store.Get(context.Background(), meta, widget.ID, &got); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Name != "gamma-updated" || got.Count != 42 {
		t.Fatalf("Get after Update = %+v, want Name=gamma-updated Count=42", got)
	}
}

func TestStoreUpdateUnknownPrimaryKeyReturnsErrNotFound(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	widget := crudWidget{ID: 999, Name: "missing"}
	err := store.Update(context.Background(), meta, &widget)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want it to wrap ErrNotFound", err)
	}
}

func TestStoreUpdateDoesNotAffectOtherRows(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	first := crudWidget{Name: "first"}
	second := crudWidget{Name: "second"}
	if err := store.Create(context.Background(), meta, &first); err != nil {
		t.Fatalf("Create first returned error: %v", err)
	}
	if err := store.Create(context.Background(), meta, &second); err != nil {
		t.Fatalf("Create second returned error: %v", err)
	}

	first.Name = "first-updated"
	if err := store.Update(context.Background(), meta, &first); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	var gotSecond crudWidget
	if err := store.Get(context.Background(), meta, second.ID, &gotSecond); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if gotSecond.Name != "second" {
		t.Fatalf("second row Name = %q, want unchanged %q", gotSecond.Name, "second")
	}
}

func TestStoreDeleteRemovesMatchingRow(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	widget := crudWidget{Name: "to-delete"}
	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if err := store.Delete(context.Background(), meta, widget.ID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	var got crudWidget
	err := store.Get(context.Background(), meta, widget.ID, &got)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete error = %v, want it to wrap ErrNotFound", err)
	}
}

func TestStoreDeleteUnknownPrimaryKeyReturnsErrNotFound(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	err := store.Delete(context.Background(), meta, int64(999))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want it to wrap ErrNotFound", err)
	}
}

func TestStoreDeleteDoesNotAffectOtherRows(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	first := crudWidget{Name: "keep"}
	second := crudWidget{Name: "remove"}
	if err := store.Create(context.Background(), meta, &first); err != nil {
		t.Fatalf("Create first returned error: %v", err)
	}
	if err := store.Create(context.Background(), meta, &second); err != nil {
		t.Fatalf("Create second returned error: %v", err)
	}

	if err := store.Delete(context.Background(), meta, second.ID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	var gotFirst crudWidget
	if err := store.Get(context.Background(), meta, first.ID, &gotFirst); err != nil {
		t.Fatalf("Get first returned error: %v", err)
	}
	if gotFirst.Name != "keep" {
		t.Fatalf("first row Name = %q, want %q", gotFirst.Name, "keep")
	}
}

func TestStoreListReturnsAllRowsByDefault(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	for _, name := range []string{"a", "b", "c"} {
		widget := crudWidget{Name: name}
		if err := store.Create(context.Background(), meta, &widget); err != nil {
			t.Fatalf("Create returned error: %v", err)
		}
	}

	var got []crudWidget
	if err := store.List(context.Background(), meta, Query{}, &got); err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List returned %d rows, want 3", len(got))
	}
}

func TestStoreListAppliesLimitAndOffset(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	for _, name := range []string{"a", "b", "c", "d"} {
		widget := crudWidget{Name: name}
		if err := store.Create(context.Background(), meta, &widget); err != nil {
			t.Fatalf("Create returned error: %v", err)
		}
	}

	var got []crudWidget
	if err := store.List(context.Background(), meta, Query{Limit: 2, Offset: 1, OrderBy: []string{"ID"}}, &got); err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d rows, want 2", len(got))
	}
	if got[0].Name != "b" || got[1].Name != "c" {
		t.Fatalf("List = %+v, want names b,c", got)
	}
}

func TestStoreListAppliesOrderByAscendingAndDescending(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	for _, count := range []int{3, 1, 2} {
		widget := crudWidget{Name: "w", Count: count}
		if err := store.Create(context.Background(), meta, &widget); err != nil {
			t.Fatalf("Create returned error: %v", err)
		}
	}

	var ascending []crudWidget
	if err := store.List(context.Background(), meta, Query{OrderBy: []string{"Count"}}, &ascending); err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(ascending) != 3 || ascending[0].Count != 1 || ascending[1].Count != 2 || ascending[2].Count != 3 {
		t.Fatalf("ascending List = %+v, want counts 1,2,3", ascending)
	}

	var descending []crudWidget
	if err := store.List(context.Background(), meta, Query{OrderBy: []string{"-Count"}}, &descending); err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(descending) != 3 || descending[0].Count != 3 || descending[1].Count != 2 || descending[2].Count != 1 {
		t.Fatalf("descending List = %+v, want counts 3,2,1", descending)
	}
}

func TestStoreListUnknownOrderByFieldFails(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	var got []crudWidget
	err := store.List(context.Background(), meta, Query{OrderBy: []string{"NoSuchField"}}, &got)
	if err == nil {
		t.Fatal("List returned nil error for unknown OrderBy field, want non-nil")
	}
}

func TestStoreListNoMatchingRowsReturnsEmptySliceNotError(t *testing.T) {
	sqlDB := openCRUDTestDB(t)
	meta := registerCRUDModel(t, crudWidget{})
	store := NewStore(sqlDB, SQLite)

	got := []crudWidget{}
	if err := store.List(context.Background(), meta, Query{}, &got); err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if got == nil {
		t.Fatal("List left dest nil, want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("List returned %d rows, want 0", len(got))
	}
}

func openCRUDTestDB(t *testing.T) *sql.DB {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	_, err = sqlDB.ExecContext(context.Background(), `
		CREATE TABLE crud_widget (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			active BOOLEAN NOT NULL,
			count INTEGER NOT NULL,
			score REAL NOT NULL,
			created_at TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	return sqlDB
}

func registerCRUDModel(t *testing.T, value any) model.ModelMeta {
	t.Helper()

	registry := model.NewRegistry()
	if err := registry.Register(value); err != nil {
		t.Fatalf("register model: %v", err)
	}

	name := reflect.TypeOf(value).Name()
	meta, ok := registry.Get(name)
	if !ok {
		t.Fatalf("registered model metadata not found for %s", name)
	}

	return meta
}
