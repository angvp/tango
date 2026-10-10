package adminregistry

import (
	"strings"
	"testing"
)

type FilterTestAuthor struct {
	ID int64 `tango:"pk"`
}

type FilterTestWeighted struct {
	ID     int64   `tango:"pk"`
	Weight float64 `tango:"fk=FilterTestAuthor"`
}

type FilterTestPost struct {
	ID       int64 `tango:"pk"`
	Title    string
	Score    float64
	Active   bool
	AuthorID int64 `tango:"fk=FilterTestAuthor"`
}

func TestRegisterAcceptsBooleanAndForeignKeyFilters(t *testing.T) {
	r := newTestRegistry(t)
	err := r.Register(FilterTestPost{}, Options{Filters: []string{"Active", "AuthorID"}})
	if err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}
}

func TestRegisterRejectsInvalidFilters(t *testing.T) {
	cases := []struct {
		name    string
		filters []string
		want    string
	}{
		{name: "unknown field", filters: []string{"Missing"}, want: `Filters references unknown field "Missing"`},
		{name: "string field", filters: []string{"Title"}, want: "Filters field \"Title\" on FilterTestPost must be a boolean or foreign key"},
		{name: "float field", filters: []string{"Score"}, want: "must be a boolean or foreign key"},
		{name: "primary key", filters: []string{"ID"}, want: "must be a boolean or foreign key"},
		{name: "duplicate", filters: []string{"Active", "Active"}, want: `Filters lists "Active" more than once`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRegistry(t)
			err := r.Register(FilterTestPost{}, Options{Filters: tc.filters})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Register() error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestRegisterRejectsForeignKeyWithUnsupportedKind(t *testing.T) {
	r := newTestRegistry(t)
	err := r.Register(FilterTestWeighted{}, Options{Filters: []string{"Weight"}})
	if err == nil || !strings.Contains(err.Error(), "must be a boolean or foreign key") {
		t.Fatalf("Register() error = %v, want filter kind validation", err)
	}
}
