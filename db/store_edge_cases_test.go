package db_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

// edgeWidget is a plain model used across the destination-validation and
// primary-key-helper cases below.
type edgeWidget struct {
	ID   int64 `tango:"pk"`
	Name string
}

// openEdgeCaseTestDB returns a fresh database holding the edge_widget
// table and the dialect to use with it.
func openEdgeCaseTestDB(t *testing.T) (*sql.DB, db.Dialect) {
	t.Helper()
	sqlDB, dialect, _ := openTables(t, edgeWidget{})
	return sqlDB, dialect
}

func edgeWidgetMeta(t *testing.T) model.ModelMeta {
	t.Helper()
	return registerModel(t, edgeWidget{})
}

// TestStoreMutatorsRejectNonPointerOrNilDestinations covers the "destination
// must be a non-nil pointer" guard shared by Create, Get, Update, List,
// QueryRow and Query.
func TestStoreMutatorsRejectNonPointerOrNilDestinations(t *testing.T) {
	sqlDB, dialect := openEdgeCaseTestDB(t)
	meta := edgeWidgetMeta(t)
	store := db.NewStore(sqlDB, dialect)
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
		{"List nil pointer", func() error { var dest *[]edgeWidget; return store.List(ctx, meta, db.Query{}, dest) }},
		{"List non-slice pointer", func() error { var dest edgeWidget; return store.List(ctx, meta, db.Query{}, &dest) }},
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
	sqlDB, dialect := testdb.Open(t)

	meta := noPKMeta()
	store := db.NewStore(sqlDB, dialect)
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
	sqlDB, dialect := openEdgeCaseTestDB(t)
	store := db.NewStore(sqlDB, dialect)
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
	sqlDB, dialect := openEdgeCaseTestDB(t)
	ctx := context.Background()
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO edge_widget (name) VALUES ('a')`); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	store := db.NewStore(sqlDB, dialect)
	meta := phantomFieldMeta()

	var got edgeWidget
	if err := store.Get(ctx, meta, int64(1), &got); err == nil {
		t.Fatal("Get scanning into a phantom field returned nil error, want an error")
	}

	var list []edgeWidget
	if err := store.List(ctx, meta, db.Query{}, &list); err == nil {
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
	sqlDB, dialect := openEdgeCaseTestDB(t)
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

	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)

	widget := edgeWidget{Name: "1"} // non-zero so validation actually runs
	if err := store.Create(ctx, referencingUnregistered, &widget); err != nil {
		t.Fatalf("Create with a foreign key to an unregistered model returned error, want it skipped: %v", err)
	}
}

// TestStoreCreateForeignKeyValidationSurfacesUnderlyingSQLError confirms
// that a genuine SQL error while checking a foreign key reference (as
// opposed to sql.ErrNoRows, which means "not found") is returned as-is
// rather than mistaken for db.ErrInvalidForeignKey.
func TestStoreCreateForeignKeyValidationSurfacesUnderlyingSQLError(t *testing.T) {
	sqlDB, dialect := openEdgeCaseTestDB(t)
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

	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)

	err := store.Create(ctx, referencing, &edgeWidget{Name: "1"})
	if err == nil {
		t.Fatal("Create returned nil error, want the underlying SQL error surfaced")
	}
	if errors.Is(err, db.ErrInvalidForeignKey) {
		t.Fatalf("error = %v, want a plain SQL error, not db.ErrInvalidForeignKey", err)
	}
}

// TestStoreMethodsSurfaceUnderlyingSQLErrorsOnClosedConnection exercises the
// ordinary SQL-error-path returns of Create, Update, Delete, List, QueryRow
// and Query by forcing every underlying database/sql call to fail: closing
// the *sql.DB out from under the Store. No live Postgres connection is
// needed — this is a plain database/sql failure mode common to every
// dialect.
func TestStoreMethodsSurfaceUnderlyingSQLErrorsOnClosedConnection(t *testing.T) {
	sqlDB, dialect := openEdgeCaseTestDB(t)
	meta := edgeWidgetMeta(t)
	ctx := context.Background()

	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	storeNoModels := db.NewStore(sqlDB, dialect)

	cases := []struct {
		name string
		call func() error
	}{
		{"Create", func() error { return storeNoModels.Create(ctx, meta, &edgeWidget{Name: "a"}) }},
		{"Get", func() error { var dest edgeWidget; return storeNoModels.Get(ctx, meta, int64(1), &dest) }},
		{"Update", func() error { return storeNoModels.Update(ctx, meta, &edgeWidget{ID: 1, Name: "a"}) }},
		{"Delete without UseModels", func() error { return storeNoModels.Delete(ctx, meta, int64(1)) }},
		{"List", func() error { var dest []edgeWidget; return storeNoModels.List(ctx, meta, db.Query{}, &dest) }},
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
	storeWithModels := db.NewStore(sqlDB, dialect)
	storeWithModels.UseModels(registry)
	if err := storeWithModels.Delete(ctx, meta, int64(1)); err == nil {
		t.Fatal("Delete (cascade path, BeginTx) on a closed connection returned nil error, want an error")
	}
}

// TestStoreQueryRowUnmatchedColumnFails is QueryRow's counterpart to
// TestStoreQueryUnmatchedColumnFails in store_raw_sql_test.go: a raw query
// selecting a column with no corresponding destination field must fail
// rather than silently drop data.
func TestStoreQueryRowUnmatchedColumnFails(t *testing.T) {
	sqlDB, dialect := openRawSQLTestDB(t)
	store := db.NewStore(sqlDB, dialect)

	var result rawAuthor
	err := store.QueryRow(
		context.Background(),
		&result,
		bind(dialect, `SELECT author.id AS id, author.name AS name, 'x' AS extra FROM author WHERE author.name = ?`),
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
	sqlDB, dialect := openEdgeCaseTestDB(t)
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

	store := db.NewStore(sqlDB, dialect)
	var list []edgeWidget
	if err := store.List(ctx, meta, db.Query{}, &list); err == nil {
		t.Fatal("List scanning a valid SQL column into a missing struct field returned nil error, want an error")
	}
}

// TestStoreDeleteCascadeExecDeleteFailureSurfacesAndRollsBack forces the
// cascade's own DELETE (as opposed to the SELECT that discovers which rows
// to cascade to) to fail, by blocking deletes on the dependent table with an
// ordinary SQL trigger — a realistic scenario (e.g. a DB-level constraint or
// audit trigger) rather than a contrived fault injection.
func TestStoreDeleteCascadeExecDeleteFailureSurfacesAndRollsBack(t *testing.T) {
	sqlDB, dialect, registry := openCascadeTestDB(t)
	ctx := context.Background()
	authorMeta, _ := registry.Get("cascadeAuthor")
	postMeta, _ := registry.Get("cascadePost")

	store := db.NewStore(sqlDB, dialect)
	store.UseModels(registry)

	author := cascadeAuthor{Name: "Jane"}
	if err := store.Create(ctx, authorMeta, &author); err != nil {
		t.Fatalf("Create author: %v", err)
	}
	post := cascadePost{AuthorID: author.ID, Title: "Hello"}
	if err := store.Create(ctx, postMeta, &post); err != nil {
		t.Fatalf("Create post: %v", err)
	}

	// The trigger's function lives in the test's own schema on PostgreSQL,
	// so testdb's schema drop removes it.
	trigger := map[db.Dialect][]string{
		db.SQLite: {`
			CREATE TRIGGER block_post_delete BEFORE DELETE ON cascade_post
			BEGIN SELECT RAISE(ABORT, 'delete blocked for test'); END;`,
		},
		db.Postgres: {`
			CREATE FUNCTION block_post_delete() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN RAISE EXCEPTION 'delete blocked for test'; END; $$`, `
			CREATE TRIGGER block_post_delete BEFORE DELETE ON cascade_post
			FOR EACH ROW EXECUTE FUNCTION block_post_delete()`,
		},
	}[dialect]
	for _, stmt := range trigger {
		if _, err := sqlDB.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
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
	if got := db.ColumnName("APIKey"); got != "api_key" {
		t.Fatalf("ColumnName(%q) = %q, want %q", "APIKey", got, "api_key")
	}
}
