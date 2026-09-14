package accounts_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/model"
	_ "modernc.org/sqlite"
)

// TestAccountHasNoPermissionShapedField locks in ADR 0020's boundary: Account
// carries exactly identity, credential, activity state, and a timestamp,
// nothing that looks like IsStaff/Role/Group/Permission.
func TestAccountHasNoPermissionShapedField(t *testing.T) {
	fieldNames := make([]string, 0)
	typ := reflect.TypeOf(accounts.Account{})
	for i := 0; i < typ.NumField(); i++ {
		fieldNames = append(fieldNames, typ.Field(i).Name)
	}

	want := []string{"ID", "Email", "PasswordHash", "Active", "CreatedAt"}
	if !reflect.DeepEqual(fieldNames, want) {
		t.Fatalf("Account fields = %v, want exactly %v (no permission-shaped field)", fieldNames, want)
	}
}

func newTestStore(t *testing.T) *db.Store {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db.NewStore(sqlDB, db.SQLite)
}

func TestNewIsInstallableViaInstalledAppsAndRegistersBothModels(t *testing.T) {
	store := newTestStore(t)

	registry, err := tango.BuildRegistry(tango.Config{
		InstalledApps: []tango.App{accounts.New(store)},
	})
	if err != nil {
		t.Fatalf("BuildRegistry returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	accountMeta, ok := registry.Models().Get("Account")
	if !ok {
		t.Fatal("Account model not registered")
	}
	sessionMeta, ok := registry.Models().Get("AccountSession")
	if !ok {
		t.Fatal("AccountSession model not registered")
	}

	assertField(t, accountMeta, "Email", func(f model.FieldMeta) bool { return f.Unique })
	assertField(t, sessionMeta, "Token", func(f model.FieldMeta) bool { return f.Unique })
	assertField(t, sessionMeta, "UserID", func(f model.FieldMeta) bool {
		return f.ForeignKey == "Account" && f.Indexed
	})
}

func assertField(t *testing.T, meta model.ModelMeta, name string, matches func(model.FieldMeta) bool) {
	t.Helper()
	for _, f := range meta.Fields {
		if f.Name == name {
			if !matches(f) {
				t.Fatalf("%s.%s did not satisfy the expected constraint: %+v", meta.Name, name, f)
			}
			return
		}
	}
	t.Fatalf("%s has no field named %s", meta.Name, name)
}

// TestAccountSessionSatisfiesAuthSessionContract proves AccountSession's
// shape is valid against auth's own reflection contract, following the
// eager-check pattern docs/guides/application-auth.md recommends.
func TestAccountSessionSatisfiesAuthSessionContract(t *testing.T) {
	registry := model.NewRegistry()
	if err := registry.Register(accounts.AccountSession{}); err != nil {
		t.Fatalf("register AccountSession: %v", err)
	}
	sessionMeta, ok := registry.Get("AccountSession")
	if !ok {
		t.Fatal("AccountSession not registered")
	}

	if err := auth.ValidateSessionModel(sessionMeta); err != nil {
		t.Fatalf("ValidateSessionModel(AccountSession) returned error: %v", err)
	}
}

// TestMigrationsProduceExpectedSchema confirms tango makemigrations'
// underlying diff engine produces the correct CreateTable steps for both
// models from a clean slate.
func TestMigrationsProduceExpectedSchema(t *testing.T) {
	registry := model.NewRegistry()
	if err := registry.Register(accounts.Account{}); err != nil {
		t.Fatalf("register Account: %v", err)
	}
	if err := registry.Register(accounts.AccountSession{}); err != nil {
		t.Fatalf("register AccountSession: %v", err)
	}

	changes := migration.Diff(registry.All(), migration.SchemaState{Tables: map[string]migration.TableState{}})

	var accountColumns, sessionColumns []migration.Column
	for _, m := range changes {
		for _, step := range m.Up {
			create, ok := step.(migration.CreateTable)
			if !ok {
				continue
			}
			switch create.Table {
			case "account":
				accountColumns = create.Columns
			case "account_session":
				sessionColumns = create.Columns
			}
		}
	}

	if accountColumns == nil {
		t.Fatal("no CreateTable step produced for the account table")
	}
	if sessionColumns == nil {
		t.Fatal("no CreateTable step produced for the account_session table")
	}

	wantColumn(t, accountColumns, "email", func(c migration.Column) bool { return c.Unique })
	wantColumn(t, sessionColumns, "token", func(c migration.Column) bool { return c.Unique })
	wantColumn(t, sessionColumns, "user_id", func(c migration.Column) bool {
		return c.References == "account" && c.Indexed
	})
}

func wantColumn(t *testing.T, columns []migration.Column, name string, matches func(migration.Column) bool) {
	t.Helper()
	for _, c := range columns {
		if c.Name == name {
			if !matches(c) {
				t.Fatalf("column %q did not satisfy the expected constraint: %+v", name, c)
			}
			return
		}
	}
	t.Fatalf("no column named %q", name)
}

// TestMigrationsApplyAndProduceExpectedSchema confirms the generated steps
// actually apply against a real database and produce the expected schema —
// the "tango migrate" half of ticket 03's acceptance criteria.
func TestMigrationsApplyAndProduceExpectedSchema(t *testing.T) {
	registry := model.NewRegistry()
	if err := registry.Register(accounts.Account{}); err != nil {
		t.Fatalf("register Account: %v", err)
	}
	if err := registry.Register(accounts.AccountSession{}); err != nil {
		t.Fatalf("register AccountSession: %v", err)
	}
	changes := migration.Diff(registry.All(), migration.SchemaState{Tables: map[string]migration.TableState{}})

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()

	ctx := context.Background()
	for _, m := range changes {
		for _, step := range m.Up {
			if err := migration.ApplyStep(ctx, sqlDB, db.SQLite, step); err != nil {
				t.Fatalf("ApplyStep(%T) returned error: %v", step, err)
			}
		}
	}

	if _, err := sqlDB.Exec(`INSERT INTO account (email, password_hash, active, created_at) VALUES ('a@example.com', 'x', 1, '2026-01-01')`); err != nil {
		t.Fatalf("insert into account: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO account_session (token, user_id, expires_at) VALUES ('tok', 1, '2026-01-01')`); err != nil {
		t.Fatalf("insert into account_session: %v", err)
	}

	if _, err := sqlDB.Exec(`INSERT INTO account (email, password_hash, active, created_at) VALUES ('a@example.com', 'y', 1, '2026-01-01')`); err == nil {
		t.Fatal("duplicate email insert succeeded, want the unique constraint to reject it")
	}
}
