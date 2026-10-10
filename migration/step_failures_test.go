package migration

import (
	"context"
	"testing"
)

// failingSteps are steps aimed at tables, columns or indexes that do not
// exist in rebuildFixture.
func failingSteps() map[string]Step {
	return map[string]Step{
		"rename a missing column":       RenameColumn{Table: "author", From: "nope", To: "other"},
		"rename a column of no table":   RenameColumn{Table: "ghost", From: "a", To: "b"},
		"rename a missing table":        RenameTable{From: "ghost", To: "phantom"},
		"rename onto an existing table": RenameTable{From: "author", To: "post"},
		"change the type of no column":  AlterColumnType{Table: "author", Column: "nope", From: "text", To: "integer"},
		"change the type of no table":   AlterColumnType{Table: "ghost", Column: "a", From: "text", To: "integer"},
		"make no column unique":         AlterColumnUnique{Table: "author", Column: "nope", Unique: true},
		"drop a missing column":         DropColumn{Table: "author", Column: "nope"},
		"drop a column of no table":     DropColumn{Table: "ghost", Column: "a"},
		"add a column to no table":      AddColumn{Table: "ghost", Column: Column{Name: "a", Type: "text"}},
		"index a missing column":        CreateIndex{Table: "author", Column: "nope"},
		"drop a missing table":          DropTable{Table: "ghost"},
	}
}

func TestStepsAimedAtSomethingMissingFailWithAnErrorAndChangeNothing(t *testing.T) {
	for name, step := range failingSteps() {
		t.Run(name, func(t *testing.T) {
			sqlDB, dialect := rebuildFixture(t)
			if err := ApplyStep(context.Background(), sqlDB, dialect, step); err == nil {
				t.Fatalf("ApplyStep(%#v) succeeded against a schema that lacks its target", step)
			}
			var n int
			if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM author`).Scan(&n); err != nil || n != 2 {
				t.Fatalf("author after the failed step: %d rows, %v; want the two seeded rows untouched", n, err)
			}
			for _, table := range []string{"author", "post"} {
				if _, err := sqlDB.Exec(`SELECT 1 FROM ` + table + ` LIMIT 1`); err != nil {
					t.Fatalf("table %s was lost by a failed step: %v", table, err)
				}
			}
		})
	}
}

func TestEveryStepFailsCleanlyOnAClosedDatabase(t *testing.T) {
	steps := []Step{
		CreateTable{Table: "t", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}},
		DropTable{Table: "author"},
		AddColumn{Table: "author", Column: Column{Name: "x", Type: "text"}},
		DropColumn{Table: "author", Column: "bio"},
		AlterColumnUnique{Table: "author", Column: "nick", Unique: true},
		CreateIndex{Table: "author", Column: "bio"},
		DropIndex{Table: "author", Column: "nick"},
		RenameColumn{Table: "author", From: "bio", To: "about"},
		RenameTable{From: "author", To: "writer"},
		AlterColumnType{Table: "author", Column: "bio", From: "text", To: "integer"},
	}
	sqlDB, dialect := rebuildFixture(t)
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if err := ApplyStep(context.Background(), sqlDB, dialect, step); err == nil {
			t.Errorf("ApplyStep(%T) on a closed database succeeded", step)
		}
	}
}
