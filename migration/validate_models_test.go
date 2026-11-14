package migration

import (
	"strings"
	"testing"

	"github.com/angvp/tango/testdb"
)

func TestValidateModels(t *testing.T) {
	testdb.SQLiteOnly(t, "pure validation test")
	authors := Model{Name: "authors", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true, Width: 32}}}
	cases := []struct {
		name   string
		models []Model
		want   string
	}{
		{"fk width matches", []Model{authors, {Name: "articles", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true, Width: 32},
			{Name: "author_id", Type: "integer", References: "authors", Width: 32},
		}}}, ""},
		{"fk width differs", []Model{authors, {Name: "articles", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "author_id", Type: "integer", References: "authors"},
		}}}, "articles.author_id is int8 but references authors's int4 primary key"},
		{"explicit names fine", []Model{{Name: "users", Columns: []Column{
			{Name: "email", Type: "text", Unique: true, UniqueName: "idx_users_email"},
		}}}, ""},
		{"explicit vs derived", []Model{{Name: "users", Columns: []Column{
			{Name: "email", Type: "text", Unique: true, UniqueName: "idx_users_slug"},
			{Name: "slug", Type: "text", Indexed: true},
		}}}, `index name "idx_users_slug"`},
		{"explicit vs table, case-insensitive", []Model{authors, {Name: "users", Columns: []Column{
			{Name: "email", Type: "text", Unique: true, UniqueName: "Authors"},
		}}}, `index name "authors"`},
		{"explicit vs pkey", []Model{{Name: "users", Columns: []Column{
			{Name: "email", Type: "text", Indexed: true, IndexName: "users_pkey"},
		}}}, `"users_pkey"`},
		{"explicit vs sequence", []Model{{Name: "users", Columns: []Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "email", Type: "text", Indexed: true, IndexName: "users_id_seq"},
		}}}, `"users_id_seq"`},
		{"explicit vs PRIMARY", []Model{{Name: "users", Columns: []Column{
			{Name: "email", Type: "text", Indexed: true, IndexName: "primary"},
		}}}, `"primary"`},
		{"explicit vs tango_migrations", []Model{{Name: "users", Columns: []Column{
			{Name: "email", Type: "text", Indexed: true, IndexName: "tango_migrations"},
		}}}, `"tango_migrations"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateModels(tc.models)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("ValidateModels = %v, want nil", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("ValidateModels = %v, want error containing %q", err, tc.want)
			}
		})
	}
}
