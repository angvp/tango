package migration

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

type compositeEntry struct {
	ID        int64 `tango:"pk"`
	AccountID int64
	Key       string
	CreatedAt time.Time
}

func compositeModels(t *testing.T, options ...model.RegisterOption) []Model {
	t.Helper()
	registry := model.NewRegistry()
	if err := registry.Register(compositeEntry{}, options...); err != nil {
		t.Fatalf("register: %v", err)
	}
	return ModelsFromMeta(registry.All())
}

func TestModelsFromMetaCarriesCompositeIndexesByColumn(t *testing.T) {
	testdb.SQLiteOnly(t, "pure conversion test")
	models := compositeModels(t,
		model.Unique("AccountID", "Key"),
		model.Index("AccountID", "CreatedAt").Named("ix_entry_recent"),
	)
	want := []CompositeIndex{
		{Columns: []string{"account_id", "created_at"}, Name: "ix_entry_recent"},
		{Columns: []string{"account_id", "key"}, Unique: true},
	}
	if !reflect.DeepEqual(models[0].Indexes, want) {
		t.Fatalf("Indexes = %#v, want %#v", models[0].Indexes, want)
	}
}

func TestModelsFromMetaSortsCompositeIndexesByEffectiveName(t *testing.T) {
	testdb.SQLiteOnly(t, "pure conversion test")
	a := compositeModels(t, model.Unique("AccountID", "Key"), model.Index("AccountID", "CreatedAt"), model.Index("Key", "AccountID"))
	b := compositeModels(t, model.Index("Key", "AccountID"), model.Index("AccountID", "CreatedAt"), model.Unique("AccountID", "Key"))
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("reordering declarations changed the models:\n%#v\n%#v", a, b)
	}
	var names []string
	for _, ix := range a[0].Indexes {
		names = append(names, compositeIndexName("composite_entry", ix))
	}
	want := []string{"idx_composite_entry_account_id_created_at", "idx_composite_entry_key_account_id", "uniq_composite_entry_account_id_key"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("effective names = %v, want %v", names, want)
	}
}

func TestModelsFromMetaTreatsTheDerivedNameAsNoName(t *testing.T) {
	testdb.SQLiteOnly(t, "pure conversion test")
	models := compositeModels(t, model.Index("AccountID", "Key").Named("idx_composite_entry_account_id_key"))
	if got := models[0].Indexes[0].Name; got != "" {
		t.Fatalf("Name = %q, want the derived name stored as empty", got)
	}
}

func TestModelJSONOmitsIndexesWhenThereAreNone(t *testing.T) {
	testdb.SQLiteOnly(t, "pure conversion test")
	data, err := json.Marshal(compositeModels(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Indexes") {
		t.Fatalf("dump of a model without declarations mentions Indexes: %s", data)
	}
}

func TestValidateModelsCompositeIndexes(t *testing.T) {
	testdb.SQLiteOnly(t, "pure validation test")
	col := func(name string) Column { return Column{Name: name, Type: "text"} }
	pk := Column{Name: "id", Type: "integer", PrimaryKey: true}
	table := func(name string, columns []Column, indexes ...CompositeIndex) Model {
		return Model{Name: name, Columns: append([]Column{pk}, columns...), Indexes: indexes}
	}
	pair := func(unique bool, columns ...string) CompositeIndex {
		return CompositeIndex{Columns: columns, Unique: unique}
	}
	long := strings.Repeat("t", 40)
	cases := []struct {
		name   string
		models []Model
		want   string
	}{
		{"valid, with prefix and reversal", []Model{table("t", []Column{col("a"), col("b"), col("c")},
			pair(false, "a", "b", "c"), pair(false, "a", "b"), pair(false, "b", "a"), pair(true, "a", "c"))}, ""},
		{"derived name too long", []Model{table(long, []Column{col("alpha_column"), col("beta_column")}, pair(false, "alpha_column", "beta_column"))},
			"is 69 bytes"},
		{"derived composite vs derived single-column", []Model{table("t", []Column{col("a"), col("b"), {Name: "a_b", Type: "text", Indexed: true}}, pair(false, "a", "b"))},
			`index name "idx_t_a_b"`},
		{"explicit composite vs another table's index, case-insensitive", []Model{
			table("u", []Column{{Name: "email", Type: "text", Indexed: true, IndexName: "Ix_shared"}}),
			table("t", []Column{col("a"), col("b")}, CompositeIndex{Columns: []string{"a", "b"}, Name: "ix_SHARED"})},
			`index name "ix_shared"`},
		{"explicit composite vs a table name", []Model{table("authors", nil),
			table("t", []Column{col("a"), col("b")}, CompositeIndex{Columns: []string{"a", "b"}, Name: "Authors"})},
			`index name "authors"`},
		{"two composites sharing an explicit name", []Model{table("t", []Column{col("a"), col("b"), col("c")},
			CompositeIndex{Columns: []string{"a", "b"}, Name: "ix_dup"}, CompositeIndex{Columns: []string{"a", "c"}, Name: "ix_dup"})},
			`index name "ix_dup"`},
		{"legacy: two derived single-column names may still collide", []Model{
			table("a", []Column{{Name: "b_c", Type: "text", Indexed: true}}),
			table("a_b", []Column{{Name: "c", Type: "text", Indexed: true}})}, ""},
		{"normal index repeats a unique tuple", []Model{table("t", []Column{col("a"), col("b")}, pair(true, "a", "b"), pair(false, "a", "b"))},
			"repeats the unique tuple"},
		{"same tuple declared twice", []Model{table("t", []Column{col("a"), col("b")},
			CompositeIndex{Columns: []string{"a", "b"}, Name: "ix_one"}, CompositeIndex{Columns: []string{"a", "b"}, Name: "ix_two"})},
			"is declared twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateModels(tc.models)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("ValidateModels = %v, want nil", err)
			case tc.want != "":
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("ValidateModels = %v, want error containing %q", err, tc.want)
				}
				if !errors.Is(err, model.ErrInvalidIndex) {
					t.Fatalf("ValidateModels = %v, want it to match model.ErrInvalidIndex", err)
				}
			}
		})
	}
}

func TestValidateModelsOnlyIndexProblemsMatchErrInvalidIndex(t *testing.T) {
	testdb.SQLiteOnly(t, "pure validation test")
	authors := Model{Name: "authors", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true, Width: 32}}}
	articles := Model{Name: "articles", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "author_id", Type: "integer", References: "authors"},
	}}
	err := ValidateModels([]Model{authors, articles})
	if err == nil || errors.Is(err, model.ErrInvalidIndex) {
		t.Fatalf("ValidateModels = %v, want a width error that does not match ErrInvalidIndex", err)
	}
}

func TestValidateModelsReportsIndexProblemsInADeterministicOrder(t *testing.T) {
	testdb.SQLiteOnly(t, "pure validation test")
	col := func(name string) Column { return Column{Name: name, Type: "text"} }
	models := []Model{{Name: "t", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}, col("a"), col("b"), col("c")}, Indexes: []CompositeIndex{
		{Columns: []string{"a", "b"}, Name: "ix_zeta"}, {Columns: []string{"a", "c"}, Name: "ix_zeta"},
		{Columns: []string{"b", "c"}, Name: "ix_alpha"}, {Columns: []string{"c", "b"}, Name: "ix_alpha"},
	}}}
	first := ValidateModels(models)
	for i := 0; i < 20; i++ {
		if again := ValidateModels(models); again == nil || again.Error() != first.Error() {
			t.Fatalf("error text changed between calls:\n%v\n%v", first, again)
		}
	}
	if !strings.Contains(first.Error(), `"ix_alpha"`) || strings.Index(first.Error(), `"ix_alpha"`) > strings.Index(first.Error(), `"ix_zeta"`) {
		t.Fatalf("problems are not sorted: %v", first)
	}
}
