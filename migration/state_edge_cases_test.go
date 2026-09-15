package migration

import (
	"strings"
	"testing"
)

// unknownStateStep is a Step implementation Replay/applyStepToState has
// never heard of, exercising the "unknown step type" default branch.
type unknownStateStep struct{}

func (unknownStateStep) isMigrationStep() {}

// TestReplayErrorPaths is a table of every error return in
// applyStepToState/requireTable/requireColumn: an already-existing table
// being created again, a missing table being dropped, a duplicate column
// being added, a missing table/column being altered, and a step type Replay
// doesn't recognize at all.
func TestReplayErrorPaths(t *testing.T) {
	baseline := []Migration{
		{Name: "0001", Up: []Step{
			CreateTable{Table: "user", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "email", Type: "text"},
			}},
		}},
	}

	cases := []struct {
		name    string
		extra   Step
		wantErr string
	}{
		{
			name:    "create table that already exists",
			extra:   CreateTable{Table: "user"},
			wantErr: `table "user" already exists`,
		},
		{
			name:    "drop table that does not exist",
			extra:   DropTable{Table: "ghost"},
			wantErr: `table "ghost" does not exist`,
		},
		{
			name:    "add column that already exists",
			extra:   AddColumn{Table: "user", Column: Column{Name: "email", Type: "text"}},
			wantErr: `column "email" already exists on table "user"`,
		},
		{
			name:    "add column to a missing table",
			extra:   AddColumn{Table: "ghost", Column: Column{Name: "x", Type: "text"}},
			wantErr: `table "ghost" does not exist`,
		},
		{
			name:    "drop column from a missing table",
			extra:   DropColumn{Table: "ghost", Column: "email"},
			wantErr: `table "ghost" does not exist`,
		},
		{
			name:    "drop a column that does not exist",
			extra:   DropColumn{Table: "user", Column: "ghost"},
			wantErr: `column "ghost" does not exist on table "user"`,
		},
		{
			name:    "alter uniqueness on a missing table",
			extra:   AlterColumnUnique{Table: "ghost", Column: "email", Unique: true},
			wantErr: `table "ghost" does not exist`,
		},
		{
			name:    "alter uniqueness on a missing column",
			extra:   AlterColumnUnique{Table: "user", Column: "ghost", Unique: true},
			wantErr: `column "ghost" does not exist on table "user"`,
		},
		{
			name:    "create index on a missing table",
			extra:   CreateIndex{Table: "ghost", Column: "email"},
			wantErr: `table "ghost" does not exist`,
		},
		{
			name:    "create index on a missing column",
			extra:   CreateIndex{Table: "user", Column: "ghost"},
			wantErr: `column "ghost" does not exist on table "user"`,
		},
		{
			name:    "drop index on a missing table",
			extra:   DropIndex{Table: "ghost", Column: "email"},
			wantErr: `table "ghost" does not exist`,
		},
		{
			name:    "drop index on a missing column",
			extra:   DropIndex{Table: "user", Column: "ghost"},
			wantErr: `column "ghost" does not exist on table "user"`,
		},
		{
			name:    "unknown step type",
			extra:   unknownStateStep{},
			wantErr: "unknown step type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			migrations := append(append([]Migration(nil), baseline...), Migration{
				Name: "0002",
				Up:   []Step{tc.extra},
			})

			_, err := Replay(migrations)
			if err == nil {
				t.Fatalf("Replay returned nil error, want an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}
