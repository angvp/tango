package db

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/angvp/tango/model"
	_ "modernc.org/sqlite"
)

// edgeWidget is a plain model used across the destination-validation and
// primary-key-helper cases below.
type edgeWidget struct {
	ID   int64 `tango:"pk"`
	Name string
}

func openEdgeCaseTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if _, err := sqlDB.ExecContext(context.Background(), `CREATE TABLE edge_widget (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return sqlDB
}

func edgeWidgetMeta(t *testing.T) model.ModelMeta {
	t.Helper()
	registry := model.NewRegistry()
	if err := registry.Register(edgeWidget{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	meta, ok := registry.Get("edgeWidget")
	if !ok {
		t.Fatal("registered model metadata not found")
	}
	return meta
}

// TestStoreMutatorsRejectNonPointerOrNilDestinations covers the "destination
// must be a non-nil pointer" guard shared by Create, Get, Update, List,
// QueryRow and Query.
func TestStoreMutatorsRejectNonPointerOrNilDestinations(t *testing.T) {
	sqlDB := openEdgeCaseTestDB(t)
	meta := edgeWidgetMeta(t)
	store := NewStore(sqlDB, SQLite)
	ctx := context.Background()

	var nilPtr *edgeWidget
	cases := []struct {
		name string
		call func() error
	}{
		{"Create nil pointer", func() error { return store.Create(ctx, meta, nilPtr) }},
		{"Create non-pointer", func() error { return store.Create(ctx, meta, edgeWidget{}) }},
		{"Get nil pointer", func() error { return store.Get(ctx, meta, int64(1), nilPtr) }},
		{"Get non-pointer", func() error { return store.Get(ctx, meta, int64(1), edgeWidget{}) }},
		{"Update nil pointer", func() error { return store.Update(ctx, meta, nilPtr) }},
		{"Update non-pointer", func() error { return store.Update(ctx, meta, edgeWidget{}) }},
		{"List nil pointer", func() error { var dest *[]edgeWidget; return store.List(ctx, meta, Query{}, dest) }},
		{"List non-slice pointer", func() error { var dest edgeWidget; return store.List(ctx, meta, Query{}, &dest) }},
		{"QueryRow nil pointer", func() error { return store.QueryRow(ctx, nilPtr, "SELECT 1") }},
		{"Query nil pointer", func() error { var dest *[]edgeWidget; return store.Query(ctx, dest, "SELECT 1") }},
		{"Query non-slice pointer", func() error { var dest edgeWidget; return store.Query(ctx, &dest, "SELECT 1") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatalf("%s: got nil error, want a validation error", tc.name)
			}
		})
	}
}

// noPKModel has no field tagged as a primary key, so findPrimaryKeyField
// must fail for it.
type noPKModel struct {
	Name string
}

func noPKMeta() model.ModelMeta {
	return model.ModelMeta{
		Name: "noPKModel",
		Type: reflect.TypeOf(noPKModel{}),
		Fields: []model.FieldMeta{
			{Name: "Name", Type: reflect.TypeOf(""), Editable: true},
		},
	}
}

// TestFindPrimaryKeyFieldErrorPropagatesThroughGetUpdateDelete confirms Get,
// Update and Delete all surface findPrimaryKeyField's error for a model with
// no primary key field, rather than panicking or silently doing nothing.
func TestFindPrimaryKeyFieldErrorPropagatesThroughGetUpdateDelete(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	meta := noPKMeta()
	store := NewStore(sqlDB, SQLite)
	ctx := context.Background()

	var dest noPKModel
	if err := store.Get(ctx, meta, "x", &dest); err == nil {
		t.Fatal("Get on a model with no primary key returned nil error, want an error")
	}
	if err := store.Update(ctx, meta, &noPKModel{Name: "a"}); err == nil {
		t.Fatal("Update on a model with no primary key returned nil error, want an error")
	}
	if err := store.Delete(ctx, meta, "x"); err == nil {
		t.Fatal("Delete on a model with no primary key returned nil error, want an error")
	}
}

// phantomFieldMeta describes a field name absent from the destination
// struct, exercising Create/Update's "field not found" guard and
// scanFieldsInto's identical guard reached through Get and List.
func phantomFieldMeta() model.ModelMeta {
	return model.ModelMeta{
		Name: "edgeWidget",
		Type: reflect.TypeOf(edgeWidget{}),
		Fields: []model.FieldMeta{
			{Name: "ID", Type: reflect.TypeOf(int64(0)), PrimaryKey: true},
			{Name: "Ghost", Type: reflect.TypeOf(""), Editable: true},
		},
	}
}

func TestStoreCreateAndUpdateFieldNotFoundReturnsError(t *testing.T) {
	sqlDB := openEdgeCaseTestDB(t)
	store := NewStore(sqlDB, SQLite)
	ctx := context.Background()
	meta := phantomFieldMeta()

	if err := store.Create(ctx, meta, &edgeWidget{Name: "a"}); err == nil {
		t.Fatal("Create with a phantom field name returned nil error, want an error")
	}
	if err := store.Update(ctx, meta, &edgeWidget{ID: 1, Name: "a"}); err == nil {
		t.Fatal("Update with a phantom field name returned nil error, want an error")
	}
}

func TestStoreGetAndListScanFieldNotFoundReturnsError(t *testing.T) {
	sqlDB := openEdgeCaseTestDB(t)
	ctx := context.Background()
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO edge_widget (name) VALUES ('a')`); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	store := NewStore(sqlDB, SQLite)
	meta := phantomFieldMeta()

	var got edgeWidget
	if err := store.Get(ctx, meta, int64(1), &got); err == nil {
		t.Fatal("Get scanning into a phantom field returned nil error, want an error")
	}

	var list []edgeWidget
	if err := store.List(ctx, meta, Query{}, &list); err == nil {
		t.Fatal("List scanning into a phantom field returned nil error, want an error")
	}
}

// TestStoreCreateForeignKeyValidationSkipsUnregisteredTarget covers
// validateForeignKeys' silent-skip branch for a foreign key pointing at a
// model name the registry doesn't know about — schema validation
// (Registry.ValidateForeignKeys) already covers that case at registration
// time, so Create/Update must not fail on it again.
//
// The sibling skip branch (a foreign key pointing at a *registered* model
// that itself has no primary key field) is not exercised here: model.Registry
// unconditionally rejects registering any model without exactly one primary
// key field (see model.ErrNoPrimaryKey), so that branch is unreachable
// through the public Registry API and would require hand-constructing a
// Registry in a way no real caller can.
func TestStoreCreateForeignKeyValidationSkipsUnregisteredTarget(t *testing.T) {
	sqlDB := openEdgeCaseTestDB(t)
	ctx := context.Background()

	registry := model.NewRegistry()
	if err := registry.Register(edgeWidget{}); err != nil {
		t.Fatalf("register edgeWidget: %v", err)
	}

	referencingUnregistered := model.ModelMeta{
		Name: "edgeWidget",
		Type: reflect.TypeOf(edgeWidget{}),
		Fields: []model.FieldMeta{
			{Name: "ID", Type: reflect.TypeOf(int64(0)), PrimaryKey: true},
			{Name: "Name", Type: reflect.TypeOf(""), Editable: true, ForeignKey: "noSuchModel"},
		},
	}

	store := NewStore(sqlDB, SQLite)
	store.UseModels(registry)

	widget := edgeWidget{Name: "1"} // non-zero so validation actually runs
	if err := store.Create(ctx, referencingUnregistered, &widget); err != nil {
		t.Fatalf("Create with a foreign key to an unregistered model returned error, want it skipped: %v", err)
	}
}

// TestStoreCreateForeignKeyValidationSurfacesUnderlyingSQLError confirms
// that a genuine SQL error while checking a foreign key reference (as
// opposed to sql.ErrNoRows, which means "not found") is returned as-is
// rather than mistaken for ErrInvalidForeignKey.
func TestStoreCreateForeignKeyValidationSurfacesUnderlyingSQLError(t *testing.T) {
	sqlDB := openEdgeCaseTestDB(t)
	ctx := context.Background()

	registry := model.NewRegistry()
	if err := registry.Register(edgeWidget{}); err != nil {
		t.Fatalf("register edgeWidget: %v", err)
	}
	// "otherModel" is registered so its FieldMeta lookup succeeds, but its
	// backing table was never created, so the existence-check SELECT fails
	// outright instead of returning sql.ErrNoRows.
	type otherModel struct {
		ID int64 `tango:"pk"`
	}
	if err := registry.Register(otherModel{}); err != nil {
		t.Fatalf("register otherModel: %v", err)
	}

	referencing := model.ModelMeta{
		Name: "edgeWidget",
		Type: reflect.TypeOf(edgeWidget{}),
		Fields: []model.FieldMeta{
			{Name: "ID", Type: reflect.TypeOf(int64(0)), PrimaryKey: true},
			{Name: "Name", Type: reflect.TypeOf(""), Editable: true, ForeignKey: "otherModel"},
		},
	}

	store := NewStore(sqlDB, SQLite)
	store.UseModels(registry)

	err := store.Create(ctx, referencing, &edgeWidget{Name: "1"})
	if err == nil {
		t.Fatal("Create returned nil error, want the underlying SQL error surfaced")
	}
	if errors.Is(err, ErrInvalidForeignKey) {
		t.Fatalf("error = %v, want a plain SQL error, not ErrInvalidForeignKey", err)
	}
}

// TestStoreMethodsSurfaceUnderlyingSQLErrorsOnClosedConnection exercises the
// ordinary SQL-error-path returns of Create, Update, Delete, List, QueryRow
// and Query by forcing every underlying database/sql call to fail: closing
// the *sql.DB out from under the Store. No live Postgres connection is
// needed — this is a plain database/sql failure mode common to every
// dialect.
func TestStoreMethodsSurfaceUnderlyingSQLErrorsOnClosedConnection(t *testing.T) {
	sqlDB := openEdgeCaseTestDB(t)
	meta := edgeWidgetMeta(t)
	ctx := context.Background()

	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	storeNoModels := NewStore(sqlDB, SQLite)

	cases := []struct {
		name string
		call func() error
	}{
		{"Create", func() error { return storeNoModels.Create(ctx, meta, &edgeWidget{Name: "a"}) }},
		{"Get", func() error { var dest edgeWidget; return storeNoModels.Get(ctx, meta, int64(1), &dest) }},
		{"Update", func() error { return storeNoModels.Update(ctx, meta, &edgeWidget{ID: 1, Name: "a"}) }},
		{"Delete without UseModels", func() error { return storeNoModels.Delete(ctx, meta, int64(1)) }},
		{"List", func() error { var dest []edgeWidget; return storeNoModels.List(ctx, meta, Query{}, &dest) }},
		{"QueryRow", func() error { var dest edgeWidget; return storeNoModels.QueryRow(ctx, &dest, "SELECT 1") }},
		{"Query", func() error { var dest []edgeWidget; return storeNoModels.Query(ctx, &dest, "SELECT 1") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatalf("%s on a closed connection returned nil error, want the closed-connection error surfaced", tc.name)
			}
		})
	}

	registry := model.NewRegistry()
	if err := registry.Register(edgeWidget{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	storeWithModels := NewStore(sqlDB, SQLite)
	storeWithModels.UseModels(registry)
	if err := storeWithModels.Delete(ctx, meta, int64(1)); err == nil {
		t.Fatal("Delete (cascade path, BeginTx) on a closed connection returned nil error, want an error")
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
	notSettable := reflect.ValueOf(edgeWidget{ID: 1}).FieldByName("ID")
	if err := setPrimaryKeyValue(notSettable, 42); err == nil {
		t.Fatal("got nil error for a non-settable value, want an error")
	}
}

// TestFindFieldByColumnUnknownColumnReturnsError is findFieldByColumn's
// direct counterpart to TestStoreQueryUnmatchedColumnFails in
// store_raw_sql_test.go, confirming the not-found path by name rather than
// through a full Query round trip.
func TestFindFieldByColumnUnknownColumnReturnsError(t *testing.T) {
	structValue := reflect.ValueOf(&edgeWidget{}).Elem()
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

// TestStoreQueryRowUnmatchedColumnFails is QueryRow's counterpart to
// TestStoreQueryUnmatchedColumnFails in store_raw_sql_test.go: a raw query
// selecting a column with no corresponding destination field must fail
// rather than silently drop data.
func TestStoreQueryRowUnmatchedColumnFails(t *testing.T) {
	sqlDB := openRawSQLTestDB(t)
	store := NewStore(sqlDB, SQLite)

	var result rawAuthor
	err := store.QueryRow(
		context.Background(),
		&result,
		`SELECT authors.id AS id, authors.name AS name, 'x' AS extra FROM authors WHERE authors.name = ?`,
		"Ada",
	)
	if err == nil {
		t.Fatal("QueryRow returned nil error for an unmatched column, want non-nil")
	}
}

// TestStoreListScanFieldNotFoundReturnsError exercises List's scanFieldsInto
// error return (as opposed to an unknown-column SQL error): the metadata
// names a field that is a valid column in the table but absent from the
// destination struct type, so the SELECT itself succeeds and the failure
// surfaces only once List tries to scan the row into the struct.
func TestStoreListScanFieldNotFoundReturnsError(t *testing.T) {
	sqlDB := openEdgeCaseTestDB(t)
	ctx := context.Background()
	if _, err := sqlDB.ExecContext(ctx, `ALTER TABLE edge_widget ADD COLUMN extra TEXT`); err != nil {
		t.Fatalf("add column: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO edge_widget (name, extra) VALUES ('a', 'z')`); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	meta := model.ModelMeta{
		Name: "edgeWidget",
		Type: reflect.TypeOf(edgeWidget{}),
		Fields: []model.FieldMeta{
			{Name: "ID", Type: reflect.TypeOf(int64(0)), PrimaryKey: true},
			{Name: "Extra", Type: reflect.TypeOf(""), Editable: true},
		},
	}

	store := NewStore(sqlDB, SQLite)
	var list []edgeWidget
	if err := store.List(ctx, meta, Query{}, &list); err == nil {
		t.Fatal("List scanning a valid SQL column into a missing struct field returned nil error, want an error")
	}
}

// TestStoreDeleteCascadeExecDeleteFailureSurfacesAndRollsBack forces the
// cascade's own DELETE (as opposed to the SELECT that discovers which rows
// to cascade to) to fail, by blocking deletes on the dependent table with an
// ordinary SQL trigger — a realistic scenario (e.g. a DB-level constraint or
// audit trigger) rather than a contrived fault injection.
func TestStoreDeleteCascadeExecDeleteFailureSurfacesAndRollsBack(t *testing.T) {
	sqlDB := openCascadeTestDB(t)
	ctx := context.Background()
	registry := cascadeTestRegistry(t)
	authorMeta, _ := registry.Get("cascadeAuthor")
	postMeta, _ := registry.Get("cascadePost")

	store := NewStore(sqlDB, SQLite)
	store.UseModels(registry)

	author := cascadeAuthor{Name: "Jane"}
	if err := store.Create(ctx, authorMeta, &author); err != nil {
		t.Fatalf("Create author: %v", err)
	}
	post := cascadePost{AuthorID: author.ID, Title: "Hello"}
	if err := store.Create(ctx, postMeta, &post); err != nil {
		t.Fatalf("Create post: %v", err)
	}

	if _, err := sqlDB.ExecContext(ctx, `
		CREATE TRIGGER block_post_delete BEFORE DELETE ON cascade_post
		BEGIN SELECT RAISE(ABORT, 'delete blocked for test'); END;
	`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if err := store.Delete(ctx, authorMeta, author.ID); err == nil {
		t.Fatal("Delete returned nil error, want the blocked cascade DELETE on cascade_post to surface")
	}

	var gotAuthor cascadeAuthor
	if err := store.Get(ctx, authorMeta, author.ID, &gotAuthor); err != nil {
		t.Fatalf("author should still exist after a rolled-back cascade failure, Get error = %v", err)
	}
}

// TestShouldInsertUnderscoreBeforeAcronymFollowedByLowercase covers the
// lookahead branch of shouldInsertUnderscore: two consecutive uppercase
// letters where the second is immediately followed by a lowercase letter
// (an acronym ending right before a new word, e.g. "APIKey").
func TestShouldInsertUnderscoreBeforeAcronymFollowedByLowercase(t *testing.T) {
	if got := ColumnName("APIKey"); got != "api_key" {
		t.Fatalf("ColumnName(%q) = %q, want %q", "APIKey", got, "api_key")
	}
}
