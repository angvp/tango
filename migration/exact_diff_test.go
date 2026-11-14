package migration

import (
	"testing"
	"time"

	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

type Users struct {
	ID        int       `tango:"pk,int4"`
	Email     string    `tango:"varchar=255,notnull,unique=idx_users_email"`
	Slug      string    `tango:"index=idx_users_slug"`
	Language  string    `tango:"varchar=5,default=en"`
	CreatedAt time.Time `tango:"naive,notnull"`
}

func exactMeta() model.ModelMeta {
	r := model.NewRegistry()
	if err := r.Register(Users{}); err != nil {
		panic(err)
	}
	meta, _ := r.Get("Users")
	meta.App = "blog"
	return meta
}

func TestModelsFromMetaCopiesExactSchemaFields(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	cols := ModelsFromMeta([]model.ModelMeta{exactMeta()})[0].Columns
	byName := map[string]Column{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	if byName["id"].Width != int4Width {
		t.Errorf("id.Width = %d, want 32", byName["id"].Width)
	}
	if c := byName["email"]; c.UniqueName != "idx_users_email" || !c.NotNull {
		t.Errorf("email = %+v", c)
	}
	if c := byName["slug"]; c.IndexName != "" || !c.Indexed {
		t.Errorf("slug = %+v, want the derived name normalized to empty", c)
	}
	if c := byName["language"]; c.DefaultKind != "string" || c.DefaultValue != "en" {
		t.Errorf("language = %+v", c)
	}
	if c := byName["created_at"]; !c.Naive || !c.NotNull {
		t.Errorf("created_at = %+v", c)
	}
}

func TestDiffExactSchemaRoundTripIsClean(t *testing.T) {
	testdb.SQLiteOnly(t, "pure diff test")
	first, err := Diff([]model.ModelMeta{exactMeta()}, SchemaState{Tables: map[string]TableState{}})
	if err != nil || len(first) != 1 {
		t.Fatalf("Diff = %v, %v", first, err)
	}
	state, err := Replay(first)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	again, err := Diff([]model.ModelMeta{exactMeta()}, state)
	if err != nil {
		t.Fatalf("second Diff: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second Diff = %+v, want no migration", again)
	}
}
