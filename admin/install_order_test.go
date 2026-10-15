package admin_test

import (
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
)

type lateNote struct {
	ID    int64 `tango:"pk"`
	Title string
}

// lateApp registers lateNote with the admin when it is installed.
func lateApp() tango.App {
	return tango.NewApp("late", func(registry *tango.Registry) error {
		if err := registry.Models().Register(lateNote{}); err != nil {
			return err
		}
		return registry.Admin().Register(lateNote{}, admin.Options{})
	})
}

func installOrderConfig(apps ...tango.App) tango.Config {
	return tango.Config{InstalledApps: apps}
}

func TestCheckFailsWhenAdminIsInstalledBeforeAnAppThatRegistersModels(t *testing.T) {
	store := db.NewStore(nil, db.SQLite)
	err := tango.Check(installOrderConfig(admin.New(store), lateApp()))
	if err == nil {
		t.Fatal("Check passed, want a failure naming the model the admin would not list")
	}
	for _, want := range []string{"lateNote", "admin.New"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Check error %q does not mention %q", err, want)
		}
	}
}

func TestCheckPassesWhenAdminIsInstalledAfterTheAppsThatRegisterModels(t *testing.T) {
	store := db.NewStore(nil, db.SQLite)
	if err := tango.Check(installOrderConfig(lateApp(), admin.New(store))); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheckPassesForAnAdminWithNoOtherApps(t *testing.T) {
	store := db.NewStore(nil, db.SQLite)
	if err := tango.Check(installOrderConfig(admin.New(store))); err != nil {
		t.Fatalf("Check: %v", err)
	}
}
