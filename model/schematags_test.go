package model

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSchemaTagsAccepted(t *testing.T) {
	stringType := reflect.TypeOf("")
	intType := reflect.TypeOf(0)
	cases := []struct {
		name      string
		tag       string
		typ       reflect.Type
		maxLength int
		want      schemaAttrs
	}{
		{"no new option", "unique,index", stringType, 0, schemaAttrs{}},
		{"int4", "int4", intType, 0, schemaAttrs{Int4: true}},
		{"int4 pk", "pk,int4", intType, 0, schemaAttrs{Int4: true}},
		{"naive", "naive", timeType, 0, schemaAttrs{Naive: true}},
		{"notnull", "notnull", stringType, 0, schemaAttrs{NotNull: true}},
		{"string default", "default=en", stringType, 0, schemaAttrs{DefaultKind: "string", DefaultValue: "en"}},
		{"string default with quote", "default=it's", stringType, 0, schemaAttrs{DefaultKind: "string", DefaultValue: "it's"}},
		{"string default at bound", "default=abcde,varchar=5", stringType, 5, schemaAttrs{DefaultKind: "string", DefaultValue: "abcde"}},
		{"multibyte default at bound", "default=ñañañ,varchar=5", stringType, 5, schemaAttrs{DefaultKind: "string", DefaultValue: "ñañañ"}},
		{"bool default", "default=false", reflect.TypeOf(true), 0, schemaAttrs{DefaultKind: "boolean", DefaultValue: "false"}},
		{"int default", "default=0", intType, 0, schemaAttrs{DefaultKind: "integer", DefaultValue: "0"}},
		{"negative int default", "default=-3", intType, 0, schemaAttrs{DefaultKind: "integer", DefaultValue: "-3"}},
		{"unique name", "unique=idx_users_email", stringType, 0, schemaAttrs{UniqueName: "idx_users_email"}},
		{"index name", "index=ix_posts_slug", stringType, 0, schemaAttrs{IndexName: "ix_posts_slug"}},
		{"value then option", "default=a,unique", stringType, 0, schemaAttrs{DefaultKind: "string", DefaultValue: "a"}},
		{"notnull with default", "notnull,default=[]", stringType, 0, schemaAttrs{NotNull: true, DefaultKind: "string", DefaultValue: "[]"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := schemaTags(tc.tag, tc.typ, tc.maxLength)
			if err != nil {
				t.Fatalf("schemaTags(%q) error = %v", tc.tag, err)
			}
			if got != tc.want {
				t.Fatalf("schemaTags(%q) = %+v, want %+v", tc.tag, got, tc.want)
			}
		})
	}
}

func TestSchemaTagsRefused(t *testing.T) {
	stringType := reflect.TypeOf("")
	intType := reflect.TypeOf(0)
	cases := []struct {
		name      string
		tag       string
		typ       reflect.Type
		maxLength int
		wantText  string
	}{
		{"int4 twice", "int4,int4", intType, 0, "int4 is declared more than once"},
		{"int4 with value", "int4=1", intType, 0, "int4 takes no value"},
		{"int4 on string", "int4", stringType, 0, "int4 applies to signed integer fields"},
		{"int4 on uint32", "int4", reflect.TypeOf(uint32(0)), 0, "int4 applies to signed integer fields"},
		{"naive on string", "naive", stringType, 0, "naive applies to time.Time fields"},
		{"naive on time twice", "naive,naive", timeType, 0, "naive is declared more than once"},
		{"notnull on pk", "pk,notnull", intType, 0, "a primary key is always NOT NULL"},
		{"empty default", "default=", stringType, 0, "default needs a value"},
		{"default on pk", "default=a,pk", stringType, 0, "a primary key cannot carry a default"},
		{"default on fk", "default=1,fk=Users", intType, 0, "a foreign key cannot carry a default"},
		{"default twice", "default=a,default=b", stringType, 0, "default is declared more than once"},
		{"backslash", `default=a\`, stringType, 0, "backslash"},
		{"stray part", "default=a,b", stringType, 0, `unrecognized part "b"`},
		{"empty part", "int4,", intType, 0, "empty part"},
		{"empty middle part", "int4, ,pk", intType, 0, "empty part"},
		{"too long", "default=toolong,varchar=5", stringType, 5, "longer than varchar=5"},
		{"bad bool", "default=yes", reflect.TypeOf(true), 0, "true or false"},
		{"bad int", "default=1.5", intType, 0, "base-10 integer"},
		{"int8 overflow", "default=300", reflect.TypeOf(int8(0)), 0, "out of range"},
		{"int4 overflow", "int4,default=3000000000", intType, 0, "out of range"},
		{"float default", "default=1", reflect.TypeOf(1.0), 0, "default applies to"},
		{"bad unique name", "unique=1bad", stringType, 0, "index name"},
		{"unique and unique=", "unique,unique=u_x", stringType, 0, "cannot be combined"},
		{"index and index=", "index,index=i_x", stringType, 0, "cannot be combined"},
		{"name too long", "unique=" + strings.Repeat("a", 64), stringType, 0, "index name"},
		{"misspelt option", "not_null,int4", intType, 0, `unrecognized part "not_null"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := schemaTags(tc.tag, tc.typ, tc.maxLength)
			if err == nil {
				t.Fatalf("schemaTags(%q) = nil error, want %q", tc.tag, tc.wantText)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("schemaTags(%q) error = %q, want it to contain %q", tc.tag, err, tc.wantText)
			}
		})
	}
}

type exactSchemaModel struct {
	ID        int       `tango:"pk,int4"`
	Email     string    `tango:"varchar=255,notnull,unique=idx_users_email"`
	Language  string    `tango:"varchar=5,default=en"`
	Published bool      `tango:"notnull,default=false"`
	Slug      string    `tango:"index=ix_slug"`
	CreatedAt time.Time `tango:"naive,notnull"`
}

func TestRegisterCopiesSchemaAttributes(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(exactSchemaModel{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	meta, _ := r.Get("exactSchemaModel")
	byName := map[string]FieldMeta{}
	for _, f := range meta.Fields {
		byName[f.Name] = f
	}
	if !byName["ID"].Int4 {
		t.Errorf("ID.Int4 = false, want true")
	}
	email := byName["Email"]
	if !email.Unique || email.UniqueName != "idx_users_email" || !email.NotNull {
		t.Errorf("Email = %+v, want Unique with UniqueName idx_users_email and NotNull", email)
	}
	if got := byName["Language"]; got.DefaultKind != "string" || got.DefaultValue != "en" || got.NotNull {
		t.Errorf("Language = %+v, want nullable string default en", got)
	}
	if got := byName["Published"]; got.DefaultKind != "boolean" || got.DefaultValue != "false" || !got.NotNull {
		t.Errorf("Published = %+v, want NOT NULL boolean default false", got)
	}
	if got := byName["Slug"]; !got.Indexed || got.IndexName != "ix_slug" {
		t.Errorf("Slug = %+v, want Indexed with IndexName ix_slug", got)
	}
	if got := byName["CreatedAt"]; !got.Naive || !got.NotNull {
		t.Errorf("CreatedAt = %+v, want Naive and NotNull", got)
	}
}

func TestRegisterWrapsSchemaTagErrors(t *testing.T) {
	type bad struct {
		ID   int    `tango:"pk"`
		Name string `tango:"int4"`
	}
	err := NewRegistry().Register(bad{})
	if !errors.Is(err, ErrInvalidFieldTag) {
		t.Fatalf("Register error = %v, want ErrInvalidFieldTag", err)
	}
	if !strings.Contains(err.Error(), "bad.Name") {
		t.Fatalf("Register error = %q, want it to name bad.Name", err)
	}
}
