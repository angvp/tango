package admin

import (
	"context"
	"database/sql"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
	_ "modernc.org/sqlite"
)

type relatedInternalAuthor struct {
	ID   int64 `tango:"pk"`
	Name string
}

func setupRelatedStore(t *testing.T) *db.Store {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if _, err := sqlDB.Exec(`CREATE TABLE related_internal_author (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO related_internal_author (id, name) VALUES (1, 'Ada')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	return db.NewStore(sqlDB, db.SQLite)
}

// TestRelatedLabelFallsBackToRawValue table-drives relatedLabel's two
// fallback branches: the foreign key's target model name isn't known to the
// model.Registry passed in at all (distinct from not being
// admin-registered, which the Label-unset branch already covers elsewhere),
// and a foreign key value that no longer has a matching row (Milestone 14,
// Q17, the related record was deleted). Both must fall back to the raw
// value rather than erroring.
func TestRelatedLabelFallsBackToRawValue(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) (*db.Store, *model.Registry, *adminregistry.Registry)
		id    int64
		want  string
	}{
		{
			name: "unregistered target model",
			setup: func(t *testing.T) (*db.Store, *model.Registry, *adminregistry.Registry) {
				store := setupRelatedStore(t)
				models := model.NewRegistry()
				adminReg := adminregistry.NewRegistry(models)
				if err := adminReg.Register(relatedInternalAuthor{}, adminregistry.Options{Label: "Name"}); err != nil {
					t.Fatalf("register: %v", err)
				}
				// A models.Registry that never saw relatedInternalAuthor at all.
				emptyModels := model.NewRegistry()
				return store, emptyModels, adminReg
			},
			id:   1,
			want: "1",
		},
		{
			name: "related row is gone",
			setup: func(t *testing.T) (*db.Store, *model.Registry, *adminregistry.Registry) {
				store := setupRelatedStore(t)
				models := model.NewRegistry()
				if err := models.Register(relatedInternalAuthor{}); err != nil {
					t.Fatalf("register model: %v", err)
				}
				adminReg := adminregistry.NewRegistry(models)
				if err := adminReg.Register(relatedInternalAuthor{}, adminregistry.Options{Label: "Name"}); err != nil {
					t.Fatalf("register: %v", err)
				}
				return store, models, adminReg
			},
			id:   999,
			want: "999",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, models, adminReg := tt.setup(t)
			got := relatedLabel(context.Background(), store, models, adminReg, "relatedInternalAuthor", tt.id)
			if got != tt.want {
				t.Fatalf("relatedLabel = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRelatedSelectOptionsFalseForUnregisteredTargetModel covers
// relatedSelectOptions' own unregistered-model guard, mirroring
// relatedLabel's: callers fall back to a plain numeric input rather than a
// broken select.
func TestRelatedSelectOptionsFalseForUnregisteredTargetModel(t *testing.T) {
	store := setupRelatedStore(t)
	models := model.NewRegistry()
	adminReg := adminregistry.NewRegistry(models)

	options, ok := relatedSelectOptions(context.Background(), store, models, adminReg, "NoSuchModel", "")
	if ok {
		t.Fatalf("relatedSelectOptions ok = true, want false for an unregistered target model")
	}
	if options != nil {
		t.Fatalf("relatedSelectOptions options = %+v, want nil", options)
	}
}
