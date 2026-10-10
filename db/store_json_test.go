package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

type JSONNote struct {
	ID    int64  `tango:"pk"`
	Title string `tango:"varchar=10"`
	Body  string `tango:"json"`
}

func openJSONNotes(t *testing.T) (*db.Store, model.ModelMeta, func(query string, args ...any)) {
	t.Helper()
	sqlDB, dialect, registry := openTables(t, JSONNote{})
	meta, _ := registry.Get("JSONNote")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := sqlDB.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	return db.NewStore(sqlDB, dialect), meta, exec
}

func TestJSONFieldAcceptsEveryJSONValueAndStoresTheExactText(t *testing.T) {
	store, meta, _ := openJSONNotes(t)
	ctx := context.Background()

	for _, body := range []string{
		`{"b": 1,   "a": [1, 2],  "a": 3}` + "\n",
		`[]`, `"text"`, `12.50`, `true`, `null`, `  {"a":1}  `,
	} {
		note := JSONNote{Body: body}
		if err := store.Create(ctx, meta, &note); err != nil {
			t.Fatalf("Create %q: %v", body, err)
		}
		var got JSONNote
		if err := store.Get(ctx, meta, note.ID, &got); err != nil {
			t.Fatal(err)
		}
		if got.Body != body {
			t.Errorf("stored %q, read back %q: the bytes must be exact", body, got.Body)
		}
	}
}

func TestJSONFieldEmptyIsUnsetAndWhitespaceOnlyIsRefused(t *testing.T) {
	store, meta, _ := openJSONNotes(t)
	ctx := context.Background()

	if err := store.Create(ctx, meta, &JSONNote{Body: ""}); err != nil {
		t.Fatalf("Create with an empty JSON field: %v", err)
	}
	for _, body := range []string{" ", "\n\t "} {
		if err := store.Create(ctx, meta, &JSONNote{Body: body}); !errors.Is(err, db.ErrInvalidJSON) {
			t.Errorf("Create %q error = %v, want ErrInvalidJSON", body, err)
		}
	}
}

func TestJSONFieldRefusesInvalidTextOnCreateAndUpdateAndWritesNothing(t *testing.T) {
	store, meta, _ := openJSONNotes(t)
	ctx := context.Background()

	err := store.Create(ctx, meta, &JSONNote{Body: `{"a":`})
	if !errors.Is(err, db.ErrInvalidJSON) {
		t.Fatalf("Create error = %v, want ErrInvalidJSON", err)
	}
	if n, _ := store.Count(ctx, meta, db.Query{}); n != 0 {
		t.Fatalf("rows after the refused Create = %d, want 0", n)
	}

	note := JSONNote{Body: `{"a":1}`}
	if err := store.Create(ctx, meta, &note); err != nil {
		t.Fatal(err)
	}
	note.Body = `{oops}`
	if err := store.Update(ctx, meta, &note); !errors.Is(err, db.ErrInvalidJSON) {
		t.Fatalf("Update error = %v, want ErrInvalidJSON", err)
	}
	var stored JSONNote
	if err := store.Get(ctx, meta, note.ID, &stored); err != nil || stored.Body != `{"a":1}` {
		t.Fatalf("stored after refused Update = %q (err %v), want the original", stored.Body, err)
	}
}

func TestInvalidJSONErrorNamesModelAndFieldButNeverTheContent(t *testing.T) {
	store, meta, _ := openJSONNotes(t)
	secret := `{"password": "hunter2",`
	err := store.Create(context.Background(), meta, &JSONNote{Body: secret})

	var invalid *db.InvalidJSONError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want a *db.InvalidJSONError", err)
	}
	if invalid.Model != "JSONNote" || invalid.Field != "Body" || invalid.Err == nil {
		t.Fatalf("error fields = %+v, want JSONNote.Body with a cause", *invalid)
	}
	if !strings.Contains(err.Error(), "JSONNote.Body") {
		t.Errorf("message %q does not name JSONNote.Body", err)
	}
	for _, leaked := range []string{"hunter2", "password"} {
		if strings.Contains(err.Error(), leaked) || strings.Contains(invalid.Err.Error(), leaked) {
			t.Errorf("error leaks submitted content %q: %v / %v", leaked, err, invalid.Err)
		}
	}
}

// Reads never validate: a row written by raw SQL (or before the tag existed)
// can be opened, listed and repaired.
func TestJSONFieldRowInvalidByRawSQLStaysReadableAndRepairable(t *testing.T) {
	store, meta, exec := openJSONNotes(t)
	ctx := context.Background()
	exec(`INSERT INTO json_note (title, body) VALUES ('old', 'not json')`)

	var rows []JSONNote
	if err := store.List(ctx, meta, db.Query{}, &rows); err != nil || len(rows) != 1 || rows[0].Body != "not json" {
		t.Fatalf("List = %+v (err %v), want the raw row", rows, err)
	}
	var got JSONNote
	if err := store.Get(ctx, meta, rows[0].ID, &got); err != nil {
		t.Fatalf("Get: %v", err)
	}

	got.Body = "still not json"
	if err := store.Update(ctx, meta, &got); !errors.Is(err, db.ErrInvalidJSON) {
		t.Fatalf("Update to another invalid value = %v, want ErrInvalidJSON", err)
	}
	got.Body = `{"fixed":true}`
	if err := store.Update(ctx, meta, &got); err != nil {
		t.Fatalf("Update to a valid value: %v", err)
	}
}

func TestJSONFieldIsCheckedInsideATransaction(t *testing.T) {
	store, meta, _ := openJSONNotes(t)
	err := store.InTx(context.Background(), func(tx *db.Store) error {
		return tx.Create(context.Background(), meta, &JSONNote{Body: "{"})
	})
	if !errors.Is(err, db.ErrInvalidJSON) {
		t.Fatalf("InTx error = %v, want ErrInvalidJSON", err)
	}
}

func TestJSONFieldEmptyIsStoredAsNull(t *testing.T) {
	store, meta, exec := openJSONNotes(t)
	if err := store.Create(context.Background(), meta, &JSONNote{Title: "unset"}); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO json_note (title, body) VALUES ('literal', 'null')`)
	// The literal null and the unset value are different stored values.
	var rows []JSONNote
	if err := store.List(context.Background(), meta, db.Query{OrderBy: []string{"ID"}}, &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].Body != "" || rows[1].Body != "null" {
		t.Fatalf("bodies = %q and %q, want \"\" and \"null\"", rows[0].Body, rows[1].Body)
	}
}
