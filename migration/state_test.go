package migration

import "testing"

// TestReplay merges TestReplayCreateTableProducesExpectedState,
// TestReplayIncrementalStepsProduceExpectedState, TestReplayDropTableRemovesTable
// and TestReplayAddColumnToMissingTableReturnsError into one table.
func TestReplay(t *testing.T) {
	cases := []struct {
		name       string
		migrations []Migration
		wantErr    bool
		check      func(t *testing.T, state SchemaState)
	}{
		{
			name: "create table produces expected state",
			migrations: []Migration{
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
			},
			check: func(t *testing.T, state SchemaState) {
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
			},
		},
		{
			name: "incremental steps across migrations produce expected final state",
			migrations: []Migration{
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
			},
			check: func(t *testing.T, state SchemaState) {
				table := state.Tables["user"]
				if len(table.Columns) != 1 || table.Columns[0].Name != "id" {
					t.Fatalf("final columns = %+v, want only id", table.Columns)
				}
			},
		},
		{
			name: "drop table removes the table from replayed state",
			migrations: []Migration{
				{Name: "0001", Up: []Step{CreateTable{Table: "user"}}},
				{Name: "0002", Up: []Step{DropTable{Table: "user"}}},
			},
			check: func(t *testing.T, state SchemaState) {
				if _, exists := state.Tables["user"]; exists {
					t.Fatalf("table %q still present after DropTable", "user")
				}
			},
		},
		{
			name: "add column to missing table returns error",
			migrations: []Migration{
				{Name: "0001", Up: []Step{
					AddColumn{Table: "ghost", Column: Column{Name: "x", Type: "text"}},
				}},
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, err := Replay(tc.migrations)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("case %q: Replay returned nil error, want error", tc.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("case %q: Replay returned error: %v", tc.name, err)
			}

			tc.check(t, state)
		})
	}
}
