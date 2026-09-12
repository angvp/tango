package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/angvp/tango/model"
	_ "modernc.org/sqlite"
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
	sqlDB := openCreateTestDB(t)
	createWidgetTable(t, sqlDB)

	meta := registerModel(t, createWidget{})
	store := NewStore(sqlDB, SQLite)
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
		"SELECT name, active, count, score, created_at FROM create_widget WHERE id = ?",
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
	sqlDB := openCreateTestDB(t)
	createWidgetTable(t, sqlDB)

	meta := registerModel(t, createWidget{})
	store := NewStore(sqlDB, SQLite)
	widget := createWidget{Name: "generated"}

	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if widget.ID == 0 {
		t.Fatal("ID = 0, want generated primary key")
	}
}

func TestStoreCreateDoesNotOverwriteSuppliedPrimaryKey(t *testing.T) {
	sqlDB := openCreateTestDB(t)
	createWidgetTable(t, sqlDB)

	meta := registerModel(t, createWidget{})
	store := NewStore(sqlDB, SQLite)
	widget := createWidget{ID: 42, Name: "supplied"}

	if err := store.Create(context.Background(), meta, &widget); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if widget.ID != 42 {
		t.Fatalf("ID = %d, want 42", widget.ID)
	}

	var id int64
	if err := sqlDB.QueryRowContext(context.Background(), "SELECT id FROM create_widget WHERE name = ?", "supplied").Scan(&id); err != nil {
		t.Fatalf("query inserted row: %v", err)
	}
	if id != 42 {
		t.Fatalf("inserted id = %d, want 42", id)
	}
}

func TestStoreCreateDerivesSnakeCaseTableAndColumnNames(t *testing.T) {
	sqlDB := openCreateTestDB(t)
	_, err := sqlDB.ExecContext(context.Background(), `
		CREATE TABLE create_user_profile (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			full_name TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	meta := registerModel(t, createUserProfile{})
	store := NewStore(sqlDB, SQLite)
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
	err = sqlDB.QueryRowContext(
		context.Background(),
		"SELECT full_name, created_at FROM create_user_profile WHERE id = ?",
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

func openCreateTestDB(t *testing.T) *sql.DB {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	return sqlDB
}

func createWidgetTable(t *testing.T, sqlDB *sql.DB) {
	t.Helper()

	_, err := sqlDB.ExecContext(context.Background(), `
		CREATE TABLE create_widget (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			active BOOLEAN NOT NULL,
			count INTEGER NOT NULL,
			score REAL NOT NULL,
			created_at TIMESTAMP NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
}

func registerModel(t *testing.T, value any) model.ModelMeta {
	t.Helper()

	registry := model.NewRegistry()
	if err := registry.Register(value); err != nil {
		t.Fatalf("register model: %v", err)
	}

	meta, ok := registry.Get(modelName(value))
	if !ok {
		t.Fatalf("registered model metadata not found")
	}

	return meta
}

func modelName(value any) string {
	switch value.(type) {
	case createWidget:
		return "createWidget"
	case createUserProfile:
		return "createUserProfile"
	default:
		return ""
	}
}
