package migration

import (
	"reflect"
	"testing"
)

func TestStepTypesCanBeUsedAsSteps(t *testing.T) {
	steps := []Step{
		CreateTable{
			Table: "user",
			Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "email", Type: "text", Unique: true, Indexed: true},
			},
		},
		DropTable{Table: "legacy_user"},
		AddColumn{
			Table:  "user",
			Column: Column{Name: "active", Type: "boolean", Indexed: true},
		},
		DropColumn{Table: "user", Column: "legacy_code"},
		AlterColumnUnique{Table: "user", Column: "email", Unique: true},
		CreateIndex{Table: "user", Column: "email"},
		DropIndex{Table: "user", Column: "email"},
	}

	if len(steps) != 7 {
		t.Fatalf("steps len = %d, want 7", len(steps))
	}
}

func TestCreateTableStoresColumnDefinitions(t *testing.T) {
	step := CreateTable{
		Table: "post",
		Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "title", Type: "text", Unique: true},
			{Name: "published_at", Type: "timestamp", Indexed: true},
		},
	}

	if step.Table != "post" {
		t.Fatalf("Table = %q, want %q", step.Table, "post")
	}

	want := []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "title", Type: "text", Unique: true},
		{Name: "published_at", Type: "timestamp", Indexed: true},
	}
	if !reflect.DeepEqual(step.Columns, want) {
		t.Fatalf("Columns = %#v, want %#v", step.Columns, want)
	}
}

func TestColumnLevelStepsStoreDocumentedFields(t *testing.T) {
	add := AddColumn{Table: "user", Column: Column{Name: "name", Type: "text"}}
	drop := DropColumn{Table: "user", Column: "name"}
	alterUnique := AlterColumnUnique{Table: "user", Column: "email", Unique: false}
	createIndex := CreateIndex{Table: "user", Column: "email"}
	dropIndex := DropIndex{Table: "user", Column: "email"}

	if add.Table != "user" || add.Column.Name != "name" || add.Column.Type != "text" {
		t.Fatalf("AddColumn = %#v, want table user and name text column", add)
	}
	if drop.Table != "user" || drop.Column != "name" {
		t.Fatalf("DropColumn = %#v, want table user and column name", drop)
	}
	if alterUnique.Table != "user" || alterUnique.Column != "email" || alterUnique.Unique {
		t.Fatalf("AlterColumnUnique = %#v, want table user, column email, unique false", alterUnique)
	}
	if createIndex.Table != "user" || createIndex.Column != "email" {
		t.Fatalf("CreateIndex = %#v, want table user and column email", createIndex)
	}
	if dropIndex.Table != "user" || dropIndex.Column != "email" {
		t.Fatalf("DropIndex = %#v, want table user and column email", dropIndex)
	}
}

func TestMigrationStoresFields(t *testing.T) {
	up := []Step{
		CreateTable{
			Table: "user",
			Columns: []Column{
				{Name: "id", Type: "integer", PrimaryKey: true},
			},
		},
		CreateIndex{Table: "user", Column: "id"},
	}
	down := []Step{
		DropIndex{Table: "user", Column: "id"},
		DropTable{Table: "user"},
	}

	migration := Migration{
		App:        "users",
		Name:       "0001_create_user",
		Up:         up,
		Down:       down,
		Reversible: true,
	}

	if migration.App != "users" {
		t.Fatalf("App = %q, want %q", migration.App, "users")
	}
	if migration.Name != "0001_create_user" {
		t.Fatalf("Name = %q, want %q", migration.Name, "0001_create_user")
	}
	if !reflect.DeepEqual(migration.Up, up) {
		t.Fatalf("Up = %#v, want %#v", migration.Up, up)
	}
	if !reflect.DeepEqual(migration.Down, down) {
		t.Fatalf("Down = %#v, want %#v", migration.Down, down)
	}
	if !migration.Reversible {
		t.Fatal("Reversible = false, want true")
	}
}
