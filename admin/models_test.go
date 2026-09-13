package admin_test

import (
	"database/sql"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	_ "modernc.org/sqlite"
)

func TestAdminRegistersAdminUserAndAdminSessionAsOrdinaryModels(t *testing.T) {
	registry := tango.NewRegistry()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := db.NewStore(sqlDB, db.SQLite)

	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}

	if _, ok := registry.Models().Get("AdminUser"); !ok {
		t.Fatal("AdminUser is not registered as a model")
	}
	if _, ok := registry.Models().Get("AdminSession"); !ok {
		t.Fatal("AdminSession is not registered as a model")
	}

	for _, registration := range registry.Admin().Registrations() {
		if registration.Model.Name == "AdminUser" || registration.Model.Name == "AdminSession" {
			t.Fatalf("%s is registered with the admin CRUD registry; it must not be", registration.Model.Name)
		}
	}

	migrations := migration.Diff(registry.Models().All(), migration.SchemaState{})

	wantTables := map[string]bool{"admin_user": false, "admin_session": false}
	for _, m := range migrations {
		for _, step := range m.Up {
			create, ok := step.(migration.CreateTable)
			if !ok {
				continue
			}
			if _, tracked := wantTables[create.Table]; tracked {
				wantTables[create.Table] = true
			}
		}
	}
	for table, found := range wantTables {
		if !found {
			t.Fatalf("no CreateTable step found for %q in generated migrations", table)
		}
	}
}
