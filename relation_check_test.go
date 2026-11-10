package tango_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/migration"
)

type relCheckAccount struct {
	ID int64 `tango:"pk"`
}

type relCheckPost struct {
	ID        int64            `tango:"pk"`
	AccountID int64            `tango:"fk=relCheckAccount"`
	Account   *relCheckAccount `tango:"belongs_to=AccountID"`
}

type relCheckWrongKeyPost struct {
	ID        int64            `tango:"pk"`
	AccountID int32            `tango:"fk=relCheckAccount"`
	Account   *relCheckAccount `tango:"belongs_to=AccountID"`
}

type relCheckPlainPost struct {
	ID        int64 `tango:"pk"`
	AccountID int64 `tango:"fk=relCheckAccount"`
}

type relCheckExtraColumnPost struct {
	ID        int64            `tango:"pk"`
	AccountID int64            `tango:"fk=relCheckAccount"`
	Title     string           `tango:"varchar=200"`
	Account   *relCheckAccount `tango:"belongs_to=AccountID"`
}

func TestCheckAcceptsRelationDeclarationRegardlessOfAppOrder(t *testing.T) {
	posts := tango.NewApp("posts", func(registry *tango.Registry) error {
		return registry.Models().Register(relCheckPost{})
	})
	accounts := tango.NewApp("accounts", func(registry *tango.Registry) error {
		return registry.Models().Register(relCheckAccount{})
	})

	if err := tango.Check(tango.Config{InstalledApps: []tango.App{posts, accounts}}); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
}

func TestCheckRefusesWrongRelationNamingModelAndField(t *testing.T) {
	posts := tango.NewApp("posts", func(registry *tango.Registry) error {
		return registry.Models().Register(relCheckWrongKeyPost{})
	})
	accounts := tango.NewApp("accounts", func(registry *tango.Registry) error {
		return registry.Models().Register(relCheckAccount{})
	})

	err := tango.Check(tango.Config{InstalledApps: []tango.App{posts, accounts}})
	if err == nil {
		t.Fatal("Check returned nil error, want the mismatched relation key refused")
	}
	for _, want := range []string{"relCheckWrongKeyPost", "Account", "AccountID"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// Relation fields are not columns, so they must not change the dumped columns
// that makemigrations diffs.
func TestDumpModelsColumnsIgnoreRelationFields(t *testing.T) {
	dump := func(value any) []migration.Column {
		t.Helper()
		app := tango.NewApp("dump", func(registry *tango.Registry) error {
			return registry.Models().Register(value)
		})
		models, err := tango.DumpModels(tango.Config{InstalledApps: []tango.App{app}})
		if err != nil {
			t.Fatalf("DumpModels returned error: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("got %d models, want 1", len(models))
		}
		return models[0].Columns
	}

	withRelation := dump(relCheckPost{})
	plain := dump(relCheckPlainPost{})
	if !reflect.DeepEqual(withRelation, plain) {
		t.Fatalf("columns with a relation field = %+v, want the plain model's %+v", withRelation, plain)
	}

	withExtraColumn := dump(relCheckExtraColumnPost{})
	if reflect.DeepEqual(withExtraColumn, plain) {
		t.Fatal("known positive failed: a model with an extra column dumped the same columns")
	}
}
