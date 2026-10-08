package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angvp/tango/migration"
)

// renamedStockModels is shopModels after Widget's Stock field became
// Quantity.
func renamedStockModels() []migration.Model {
	models := shopModels(false, false)
	models[0].Columns[1].Name = "quantity"
	models[0].Fields = map[string]string{"id": "ID", "quantity": "Quantity"}
	return models
}

func TestMakeMigrationsRenamesAFieldInsteadOfDroppingIt(t *testing.T) {
	dir := t.TempDir()
	makeInitialMigration(t, dir, shopModels(false, false))

	code, stderr := runMakeMigrations(t, dir, renamedStockModels(), "--rename", "shop.Widget.Stock=Quantity")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr)
	}
	files, err := filepath.Glob(filepath.Join(dir, "migrations", "0002_*.go"))
	if err != nil || len(files) != 1 {
		t.Fatalf("second migration files = %v (%v), want one", files, err)
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	for _, want := range []string{
		`migration.RenameColumn{Table: "widget", From: "stock", To: "quantity"}`,
		`migration.RenameColumn{Table: "widget", From: "quantity", To: "stock"}`,
		"Reversible: true",
	} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("migration does not contain %q:\n%s", want, content)
		}
	}
	if strings.Contains(string(content), "DropColumn") {
		t.Fatalf("migration drops a column although the field was renamed:\n%s", content)
	}

	// The next run reads the rename back from the file's JSON and replays
	// it, so it finds nothing left to do.
	var stdout strings.Builder
	if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &strings.Builder{}, dumpModelsRunner{models: renamedStockModels()}); code != 0 {
		t.Fatalf("follow-up run exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "no changes detected") {
		t.Fatalf("follow-up run stdout = %q, want no changes detected", stdout.String())
	}
}

func TestMakeMigrationsRefusesARenameThatDoesNotFit(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		wants []string
	}{
		{"source not in history", []string{"--rename", "shop.Widget.Stok=Quantity"}, []string{"shop.widget.stok", "not in migration history"}},
		{"destination not in the models", []string{"--rename", "shop.Widget.Stock=Qty"}, []string{"shop.widget.qty", "no field", "ID, Quantity"}},
		{"missing =", []string{"--rename", "shop.Widget.Stock"}, []string{`"shop.Widget.Stock"`, "app.Model.Field=NewField"}},
		{"empty destination", []string{"--rename", "shop.Widget.Stock="}, []string{`"shop.Widget.Stock="`, "app.Model.Field=NewField"}},
		{"destination with a path", []string{"--rename", "shop.Widget.Stock=shop.Widget.Quantity"}, []string{"app.Model.Field=NewField"}},
		{"source without a field", []string{"--rename", "Stock=Quantity"}, []string{`"Stock=Quantity"`, "app.Model.Field=NewField"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			makeInitialMigration(t, dir, shopModels(false, false))
			before := migrationsDirContents(t, dir)

			code, stderr := runMakeMigrations(t, dir, renamedStockModels(), tt.args...)

			assertRefusedAndUnchanged(t, dir, before, code, stderr, tt.wants...)
		})
	}
}

func TestUsageDocumentsRename(t *testing.T) {
	var stdout strings.Builder
	Run(context.Background(), []string{"help"}, t.TempDir(), &stdout, &strings.Builder{}, dumpModelsRunner{})
	if !strings.Contains(stdout.String(), "--rename <app.Model.Field=NewField>") {
		t.Fatalf("usage does not document --rename:\n%s", stdout.String())
	}
}

func TestMakeMigrationsWidensAFieldAndRoundTripsTheStep(t *testing.T) {
	dir := t.TempDir()
	makeInitialMigration(t, dir, shopModels(false, false))
	widened := renamedStockModels()
	widened[0].Columns[1].Type = "real"

	code, stderr := runMakeMigrations(t, dir, widened, "--rename", "shop.Widget.Stock=Quantity")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr)
	}
	files, err := filepath.Glob(filepath.Join(dir, "migrations", "0002_*.go"))
	if err != nil || len(files) != 1 {
		t.Fatalf("second migration files = %v (%v), want one", files, err)
	}
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	rename := strings.Index(string(content), `migration.RenameColumn{Table: "widget", From: "stock", To: "quantity"}`)
	alter := strings.Index(string(content), `migration.AlterColumnType{Table: "widget", Column: "quantity", From: "integer", To: "real"}`)
	if rename == -1 || alter == -1 || rename > alter || !strings.Contains(string(content), "Reversible: false") {
		t.Fatalf("migration does not rename then widen, irreversibly:\n%s", content)
	}

	var stdout strings.Builder
	if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &strings.Builder{}, dumpModelsRunner{models: widened}); code != 0 {
		t.Fatalf("follow-up run exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "no changes detected") {
		t.Fatalf("follow-up run stdout = %q, want no changes detected", stdout.String())
	}
}

// gizmoModels is shopModels after the Widget model became Gizmo, with its
// Stock field named stockName.
func gizmoModels(stockName, stockField string) []migration.Model {
	models := shopModels(false, false)
	models[0].Name = "gizmo"
	models[0].Struct = "Gizmo"
	models[0].Columns[1].Name = stockName
	models[0].Fields = map[string]string{"id": "ID", stockName: stockField}
	return models
}

func TestMakeMigrationsRenamesAModel(t *testing.T) {
	tests := []struct {
		name   string
		models []migration.Model
		args   []string
		wants  []string
	}{
		{"model", gizmoModels("stock", "Stock"), []string{"--rename", "shop.Widget=Gizmo"},
			[]string{`migration.RenameTable{From: "widget", To: "gizmo"}`}},
		{"model and field by the model's old name", gizmoModels("quantity", "Quantity"),
			[]string{"--rename", "shop.Widget=Gizmo", "--rename", "shop.Widget.Stock=Quantity"},
			[]string{`migration.RenameTable{From: "widget", To: "gizmo"}`, `migration.RenameColumn{Table: "gizmo", From: "stock", To: "quantity"}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			makeInitialMigration(t, dir, shopModels(false, false))

			code, stderr := runMakeMigrations(t, dir, tt.models, tt.args...)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr)
			}
			files, err := filepath.Glob(filepath.Join(dir, "migrations", "0002_*.go"))
			if err != nil || len(files) != 1 {
				t.Fatalf("second migration files = %v (%v), want one", files, err)
			}
			content, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatalf("read migration: %v", err)
			}
			for _, want := range append(tt.wants, "Reversible: true") {
				if !strings.Contains(string(content), want) {
					t.Fatalf("migration does not contain %q:\n%s", want, content)
				}
			}

			var stdout strings.Builder
			if code := Run(context.Background(), []string{"makemigrations"}, dir, &stdout, &strings.Builder{}, dumpModelsRunner{models: tt.models}); code != 0 {
				t.Fatalf("follow-up run exit code = %d, want 0", code)
			}
			if !strings.Contains(stdout.String(), "no changes detected") {
				t.Fatalf("follow-up run stdout = %q, want no changes detected", stdout.String())
			}
		})
	}
}

func TestMakeMigrationsRefusesModelRenamesThatOverlap(t *testing.T) {
	tests := []struct {
		name   string
		models []migration.Model
		args   []string
		wants  []string
	}{
		{"field named by the model's new name", gizmoModels("quantity", "Quantity"),
			[]string{"--rename", "shop.Widget=Gizmo", "--rename", "shop.Gizmo.Stock=Quantity"},
			[]string{"shop.gizmo.stock", "old name", "shop.widget"}},
		{"renamed model also authorised for drop", gizmoModels("stock", "Stock"),
			[]string{"--rename", "shop.Widget=Gizmo", "--allow-drop", "shop.Widget"},
			[]string{"shop.Widget", "not dropped"}},
		{"field rename on a dropped model", shopModels(false, false)[1:],
			[]string{"--allow-drop", "shop.Widget", "--rename", "shop.Widget.Stock=Quantity"},
			[]string{"shop.widget.stock", "no table widget"}},
		{"two models renamed to one", gizmoModels("stock", "Stock"),
			[]string{"--rename", "shop.Widget=Gizmo", "--rename", "shop.Gadget=Gizmo"},
			[]string{"shop.gizmo", "more than once"}},
		{"chained model renames", gizmoModels("stock", "Stock"),
			[]string{"--rename", "shop.Widget=Gadget", "--rename", "shop.Gadget=Gizmo"},
			[]string{"shop.gadget", "both"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			makeInitialMigration(t, dir, shopModels(false, false))
			before := migrationsDirContents(t, dir)

			code, stderr := runMakeMigrations(t, dir, tt.models, tt.args...)

			assertRefusedAndUnchanged(t, dir, before, code, stderr, tt.wants...)
		})
	}
}
