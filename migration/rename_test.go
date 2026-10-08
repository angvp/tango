package migration

import (
	"context"
	"slices"
	"testing"

	"github.com/angvp/tango/testdb"
)

func TestRenameColumnKeepsTheColumnsDataConstraintsAndIndexes(t *testing.T) {
	sqlDB, dialect := rebuildFixture(t)
	applySteps(t, sqlDB, dialect,
		RenameColumn{Table: "author", From: "email", To: "contact"},
		RenameColumn{Table: "author", From: "nick", To: "handle"},
		RenameColumn{Table: "post", From: "author_id", To: "writer_id"},
	)

	var contact, handle string
	if err := sqlDB.QueryRow(`SELECT contact, handle FROM author WHERE id = 1`).Scan(&contact, &handle); err != nil {
		t.Fatalf("read renamed columns: %v", err)
	}
	if contact != "ada@example.com" || handle != "ada" {
		t.Fatalf("contact, handle = %q, %q; want the values the old columns held", contact, handle)
	}
	if got, want := indexNames(t, sqlDB, dialect, "author"), []string{"idx_author_handle", "uniq_author_contact"}; !slices.Equal(got, want) {
		t.Fatalf("author indexes = %v, want %v (named after the new columns)", got, want)
	}
	if got, want := indexNames(t, sqlDB, dialect, "post"), []string{"idx_post_writer_id"}; !slices.Equal(got, want) {
		t.Fatalf("post indexes = %v, want %v", got, want)
	}
	mustFail(t, sqlDB, "contact is still unique", `INSERT INTO author (contact, handle) VALUES ('ada@example.com', 'other')`)
	mustFail(t, sqlDB, "writer_id still references author", `INSERT INTO post (writer_id, title) VALUES (999, 'orphan')`)
	mustExec(t, sqlDB, `INSERT INTO post (writer_id, title) VALUES (2, 'Engines')`)

	// The renamed index can be dropped and recreated by the column's new
	// name, as a later migration would.
	applySteps(t, sqlDB, dialect,
		DropIndex{Table: "author", Column: "handle"},
		AlterColumnUnique{Table: "author", Column: "contact", Unique: false},
	)
}

func TestRenameColumnUndoesItselfInReverse(t *testing.T) {
	sqlDB, dialect := rebuildFixture(t)
	indexesBefore := indexNames(t, sqlDB, dialect, "author")

	applySteps(t, sqlDB, dialect,
		RenameColumn{Table: "author", From: "email", To: "contact"},
		RenameColumn{Table: "author", From: "contact", To: "email"},
	)

	var email string
	if err := sqlDB.QueryRow(`SELECT email FROM author WHERE id = 2`).Scan(&email); err != nil {
		t.Fatalf("read email: %v", err)
	}
	if email != "alan@example.com" {
		t.Fatalf("email = %q, want the original value", email)
	}
	if got := indexNames(t, sqlDB, dialect, "author"); !slices.Equal(got, indexesBefore) {
		t.Fatalf("author indexes = %v, want %v", got, indexesBefore)
	}
}

func TestMigrateDownRestoresARenamedFieldsName(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	initial := Migration{App: "posts", Name: "0001_initial", Reversible: true, Up: []Step{CreateTable{Table: "post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "body", Type: "text", Indexed: true},
	}}}, Down: []Step{DropTable{Table: "post"}}}
	renames, err := DiffModels([]Model{{App: "posts", Name: "post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "content", Type: "text", Indexed: true},
	}}}, mustReplay(t, initial), Rename{App: "posts", Table: "post", Column: "body", To: "content"})
	if err != nil {
		t.Fatalf("DiffModels: %v", err)
	}
	renames[0].Name = "0002_rename"
	all := []Migration{initial, renames[0]}

	if err := ApplyPending(ctx, sqlDB, dialect, all[:1]); err != nil {
		t.Fatalf("apply initial: %v", err)
	}
	mustExec(t, sqlDB, `INSERT INTO post (body) VALUES ('kept')`)
	if err := ApplyPending(ctx, sqlDB, dialect, all); err != nil {
		t.Fatalf("apply rename: %v", err)
	}
	if err := RollbackLast(ctx, sqlDB, dialect, all); err != nil {
		t.Fatalf("RollbackLast: %v", err)
	}

	var body string
	if err := sqlDB.QueryRow(`SELECT body FROM post`).Scan(&body); err != nil {
		t.Fatalf("read body after rollback: %v", err)
	}
	if body != "kept" {
		t.Fatalf("body = %q, want %q", body, "kept")
	}
	if got, want := indexNames(t, sqlDB, dialect, "post"), []string{"idx_post_body"}; !slices.Equal(got, want) {
		t.Fatalf("post indexes = %v, want %v", got, want)
	}
}

func mustReplay(t *testing.T, migrations ...Migration) SchemaState {
	t.Helper()
	state, err := Replay(migrations)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	return state
}

func TestRenameTableKeepsRowsIndexesAndIncomingForeignKeys(t *testing.T) {
	sqlDB, dialect := rebuildFixture(t)

	applySteps(t, sqlDB, dialect, RenameTable{From: "author", To: "writer"})

	var email string
	if err := sqlDB.QueryRow(`SELECT email FROM writer WHERE id = 2`).Scan(&email); err != nil {
		t.Fatalf("read renamed table: %v", err)
	}
	if email != "alan@example.com" {
		t.Fatalf("email = %q, want the row's original value", email)
	}
	if got, want := indexNames(t, sqlDB, dialect, "writer"), []string{"idx_writer_nick", "uniq_writer_email"}; !slices.Equal(got, want) {
		t.Fatalf("writer indexes = %v, want %v (named after the new table)", got, want)
	}
	mustFail(t, sqlDB, "email is still unique", `INSERT INTO writer (email, nick) VALUES ('ada@example.com', 'other')`)
	mustFail(t, sqlDB, "post still references the renamed table", `INSERT INTO post (author_id, title) VALUES (999, 'orphan')`)
	mustFail(t, sqlDB, "posts still reference writer 1", `DELETE FROM writer WHERE id = 1`)
	mustExec(t, sqlDB, `INSERT INTO post (author_id, title) VALUES (2, 'Engines')`)
	mustExec(t, sqlDB, `INSERT INTO writer (email, nick) VALUES ('grace@example.com', 'grace')`)

	// Indexes are found by the new table's name, as a later migration would.
	applySteps(t, sqlDB, dialect,
		DropIndex{Table: "writer", Column: "nick"},
		AlterColumnUnique{Table: "writer", Column: "email", Unique: false},
	)
}

func TestRenameTableUndoesItselfInReverse(t *testing.T) {
	sqlDB, dialect := rebuildFixture(t)
	indexesBefore := indexNames(t, sqlDB, dialect, "author")

	applySteps(t, sqlDB, dialect,
		RenameTable{From: "author", To: "writer"},
		RenameTable{From: "writer", To: "author"},
	)

	if got := indexNames(t, sqlDB, dialect, "author"); !slices.Equal(got, indexesBefore) {
		t.Fatalf("author indexes = %v, want %v", got, indexesBefore)
	}
	mustFail(t, sqlDB, "post references author again", `INSERT INTO post (author_id, title) VALUES (999, 'orphan')`)
	mustExec(t, sqlDB, `INSERT INTO post (author_id, title) VALUES (1, 'Back')`)
}
