package migration

import "testing"

func TestReplayCreateTableProducesExpectedState(t *testing.T) {
	migrations := []Migration{
		{
			Name: "0001_create_user",
			App:  "users",
			Up: []Step{
				CreateTable{Table: "user", Columns: []Column{
					{Name: "id", Type: "integer", PrimaryKey: true},
					{Name: "email", Type: "text", Unique: true},
				}},
			},
		},
	}

	state, err := Replay(migrations)
	if err != nil {
		t.Fatalf("Replay returned error: %v", err)
	}

	table, ok := state.Tables["user"]
	if !ok {
		t.Fatalf("table %q not present in replayed state", "user")
	}
	if len(table.Columns) != 2 {
		t.Fatalf("got %d columns, want 2", len(table.Columns))
	}
	if table.App != "users" {
		t.Fatalf("table App = %q, want %q", table.App, "users")
	}
}

func TestReplayIncrementalStepsProduceExpectedState(t *testing.T) {
	migrations := []Migration{
		{Name: "0001", App: "users", Up: []Step{
			CreateTable{Table: "user", Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
			}},
		}},
		{Name: "0002", App: "users", Up: []Step{
			AddColumn{Table: "user", Column: Column{Name: "email", Type: "text"}},
		}},
		{Name: "0003", App: "users", Up: []Step{
			AlterColumnUnique{Table: "user", Column: "email", Unique: true},
		}},
		{Name: "0004", App: "users", Up: []Step{
			CreateIndex{Table: "user", Column: "email"},
		}},
		{Name: "0005", App: "users", Up: []Step{
			DropIndex{Table: "user", Column: "email"},
		}},
		{Name: "0006", App: "users", Up: []Step{
			DropColumn{Table: "user", Column: "email"},
		}},
	}

	state, err := Replay(migrations)
	if err != nil {
		t.Fatalf("Replay returned error: %v", err)
	}

	table := state.Tables["user"]
	if len(table.Columns) != 1 || table.Columns[0].Name != "id" {
		t.Fatalf("final columns = %+v, want only id", table.Columns)
	}
}

func TestReplayDropTableRemovesTable(t *testing.T) {
	migrations := []Migration{
		{Name: "0001", Up: []Step{CreateTable{Table: "user"}}},
		{Name: "0002", Up: []Step{DropTable{Table: "user"}}},
	}

	state, err := Replay(migrations)
	if err != nil {
		t.Fatalf("Replay returned error: %v", err)
	}

	if _, exists := state.Tables["user"]; exists {
		t.Fatalf("table %q still present after DropTable", "user")
	}
}

func TestReplayAddColumnToMissingTableReturnsError(t *testing.T) {
	migrations := []Migration{
		{Name: "0001", Up: []Step{
			AddColumn{Table: "ghost", Column: Column{Name: "x", Type: "text"}},
		}},
	}

	if _, err := Replay(migrations); err == nil {
		t.Fatalf("Replay returned nil error, want error for AddColumn on missing table")
	}
}
