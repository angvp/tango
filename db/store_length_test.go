package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

type Headline struct {
	ID    int64  `tango:"pk"`
	Title string `tango:"varchar=10"`
	Slug  string `tango:"varchar=5"`
	Note  string
}

func openHeadlines(t *testing.T) (*db.Store, model.ModelMeta) {
	t.Helper()
	sqlDB, dialect, registry := openTables(t, Headline{})
	meta, _ := registry.Get("Headline")
	return db.NewStore(sqlDB, dialect), meta
}

func countHeadlines(t *testing.T, store *db.Store, meta model.ModelMeta) int {
	t.Helper()
	n, err := store.Count(context.Background(), meta, db.Query{})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateAcceptsABoundedStringUpToItsLimitAndRefusesOneMore(t *testing.T) {
	store, meta := openHeadlines(t)
	ctx := context.Background()

	if err := store.Create(ctx, meta, &Headline{Title: strings.Repeat("a", 10)}); err != nil {
		t.Fatalf("Create with exactly 10 characters: %v", err)
	}
	if err := store.Create(ctx, meta, &Headline{Title: ""}); err != nil {
		t.Fatalf("Create with an empty string: %v", err)
	}
	err := store.Create(ctx, meta, &Headline{Title: strings.Repeat("a", 11)})
	if !errors.Is(err, db.ErrValueTooLong) {
		t.Fatalf("Create with 11 characters error = %v, want ErrValueTooLong", err)
	}
	if got := countHeadlines(t, store, meta); got != 2 {
		t.Fatalf("rows after the refused Create = %d, want 2: nothing may be written", got)
	}
}

func TestTheErrorNamesModelFieldAndBothLengths(t *testing.T) {
	store, meta := openHeadlines(t)
	err := store.Create(context.Background(), meta, &Headline{Title: strings.Repeat("a", 13)})

	var tooLong *db.ValueTooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("error = %v, want a *db.ValueTooLongError", err)
	}
	if tooLong.Model != "Headline" || tooLong.Field != "Title" || tooLong.Max != 10 || tooLong.Got != 13 {
		t.Fatalf("error fields = %+v, want Headline.Title max 10 got 13", *tooLong)
	}
	for _, want := range []string{"Headline.Title", "13", "10"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err, want)
		}
	}
}

func TestLengthIsCountedInRunesNotBytesOrGraphemes(t *testing.T) {
	store, meta := openHeadlines(t)
	ctx := context.Background()
	tests := []struct {
		name  string
		value string
		fits  bool
	}{
		{"ten accented letters (20 bytes)", strings.Repeat("é", 10), true},
		{"eleven accented letters", strings.Repeat("é", 11), false},
		{"ten emoji (40 bytes)", strings.Repeat("😀", 10), true},
		{"eleven emoji", strings.Repeat("😀", 11), false},
		// A base letter plus a combining accent is two runes but looks like
		// one character: runes are not graphemes.
		{"five letters with combining accents", strings.Repeat("é", 5), true},
		{"six letters with combining accents", strings.Repeat("é", 6), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.Create(ctx, meta, &Headline{Title: tt.value})
			if tt.fits && err != nil {
				t.Fatalf("Create: %v, want it accepted", err)
			}
			if !tt.fits && !errors.Is(err, db.ErrValueTooLong) {
				t.Fatalf("Create error = %v, want ErrValueTooLong", err)
			}
		})
	}
}

func TestEachInvalidUTF8ByteCountsAsOneRune(t *testing.T) {
	store, meta := openHeadlines(t)
	ctx := context.Background()

	// Eleven bad bytes are eleven runes: refused by the length rule before any
	// SQL runs, on every dialect.
	err := store.Create(ctx, meta, &Headline{Title: strings.Repeat("\xff", 11)})
	if !errors.Is(err, db.ErrValueTooLong) {
		t.Fatalf("11 invalid bytes error = %v, want ErrValueTooLong", err)
	}
	var tooLong *db.ValueTooLongError
	if !errors.As(err, &tooLong) || tooLong.Got != 11 {
		t.Fatalf("counted %+v, want 11 runes for 11 invalid bytes", tooLong)
	}
	// Ten are within the limit as far as the length rule goes. Whether the
	// database then accepts invalid UTF-8 is its own business (PostgreSQL
	// refuses it), but it must not be reported as too long.
	err = store.Create(ctx, meta, &Headline{Title: strings.Repeat("\xff", 10)})
	if errors.Is(err, db.ErrValueTooLong) {
		t.Fatalf("10 invalid bytes were reported too long: %v", err)
	}
}

func TestUpdateRefusesAnOverlongValueAndLeavesTheRowAlone(t *testing.T) {
	store, meta := openHeadlines(t)
	ctx := context.Background()
	headline := Headline{Title: "short", Slug: "ab"}
	if err := store.Create(ctx, meta, &headline); err != nil {
		t.Fatal(err)
	}

	headline.Title = strings.Repeat("x", 11)
	if err := store.Update(ctx, meta, &headline); !errors.Is(err, db.ErrValueTooLong) {
		t.Fatalf("Update error = %v, want ErrValueTooLong", err)
	}
	var got Headline
	if err := store.Get(ctx, meta, headline.ID, &got); err != nil || got.Title != "short" {
		t.Fatalf("row after the refused Update = %+v (%v), want Title unchanged", got, err)
	}

	headline.Title = strings.Repeat("x", 10)
	if err := store.Update(ctx, meta, &headline); err != nil {
		t.Fatalf("Update with exactly 10 characters: %v", err)
	}
}

func TestOnlyBoundedFieldsAreValidatedAndTheFirstOffenderIsReported(t *testing.T) {
	store, meta := openHeadlines(t)
	ctx := context.Background()

	if err := store.Create(ctx, meta, &Headline{Note: strings.Repeat("n", 100000)}); err != nil {
		t.Fatalf("an unbounded string was refused: %v", err)
	}
	err := store.Create(ctx, meta, &Headline{Title: strings.Repeat("a", 11), Slug: strings.Repeat("b", 9)})
	var tooLong *db.ValueTooLongError
	if !errors.As(err, &tooLong) || tooLong.Field != "Title" {
		t.Fatalf("error = %v, want the first offending field in declaration order, Title", err)
	}
}
