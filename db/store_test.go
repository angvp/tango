package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/angvp/tango/db"
)

type createWidget struct {
	ID        int64 `tango:"pk"`
	Name      string
	Active    bool
	Count     int
	Score     float64
	CreatedAt time.Time
}

type createUserProfile struct {
	ID        int64 `tango:"pk"`
	FullName  string
	CreatedAt time.Time
}

func TestStoreCreateInsertsRow(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, createWidget{})
	meta := metaFor(t, registry, createWidget{})
	store := db.NewStore(sqlDB, dialect)
	createdAt := time.Date(2026, 9, 11, 12, 30, 0, 0, time.UTC)
	widget := createWidget{
		Name:      "alpha",
		Active:    true,
		Count:     7,
		Score:     9.5,
		CreatedAt: createdAt,
	}

	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	var name string
	var active bool
	var count int
	var score float64
	var gotCreatedAt time.Time
	err := sqlDB.QueryRowContext(
		context.Background(),
		bind(dialect, "SELECT name, active, count, score, created_at FROM create_widget WHERE id = ?"),
		widget.ID,
	).Scan(&name, &active, &count, &score, &gotCreatedAt)
	if err != nil {
		t.Fatalf("query inserted row: %v", err)
	}

	if name != widget.Name {
		t.Fatalf("name = %q, want %q", name, widget.Name)
	}
	if active != widget.Active {
		t.Fatalf("active = %v, want %v", active, widget.Active)
	}
	if count != widget.Count {
		t.Fatalf("count = %d, want %d", count, widget.Count)
	}
	if score != widget.Score {
		t.Fatalf("score = %v, want %v", score, widget.Score)
	}
	if !gotCreatedAt.Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v", gotCreatedAt, createdAt)
	}
}

func TestStoreCreateBackfillsGeneratedPrimaryKey(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, createWidget{})
	meta := metaFor(t, registry, createWidget{})
	store := db.NewStore(sqlDB, dialect)
	widget := createWidget{Name: "generated"}

	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if widget.ID == 0 {
		t.Fatal("ID = 0, want generated primary key")
	}
}

func TestStoreCreateDoesNotOverwriteSuppliedPrimaryKey(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, createWidget{})
	meta := metaFor(t, registry, createWidget{})
	store := db.NewStore(sqlDB, dialect)
	widget := createWidget{ID: 42, Name: "supplied"}

	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if widget.ID != 42 {
		t.Fatalf("ID = %d, want 42", widget.ID)
	}

	var id int64
	if err := sqlDB.QueryRowContext(context.Background(), bind(dialect, "SELECT id FROM create_widget WHERE name = ?"), "supplied").Scan(&id); err != nil {
		t.Fatalf("query inserted row: %v", err)
	}
	if id != 42 {
		t.Fatalf("inserted id = %d, want 42", id)
	}
}

func TestStoreCreateDerivesSnakeCaseTableAndColumnNames(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, createUserProfile{})
	meta := metaFor(t, registry, createUserProfile{})
	store := db.NewStore(sqlDB, dialect)
	createdAt := time.Date(2026, 9, 11, 14, 0, 0, 0, time.UTC)
	profile := createUserProfile{
		FullName:  "Ada Lovelace",
		CreatedAt: createdAt,
	}

	if err := store.Create(context.Background(), meta, &profile); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	var fullName string
	var gotCreatedAt time.Time
	err := sqlDB.QueryRowContext(
		context.Background(),
		bind(dialect, "SELECT full_name, created_at FROM create_user_profile WHERE id = ?"),
		profile.ID,
	).Scan(&fullName, &gotCreatedAt)
	if err != nil {
		t.Fatalf("query inserted row: %v", err)
	}

	if fullName != profile.FullName {
		t.Fatalf("full_name = %q, want %q", fullName, profile.FullName)
	}
	if !gotCreatedAt.Equal(createdAt) {
		t.Fatalf("created_at = %v, want %v", gotCreatedAt, createdAt)
	}
}
