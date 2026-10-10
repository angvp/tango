package model

import (
	"errors"
	"strings"
	"testing"
)

type jsonDoc struct {
	ID    int64  `tango:"pk"`
	Prefs string `tango:"json"`
	Plain string
}

func TestJSONTagMarksOnlyThatFieldAsJSON(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(jsonDoc{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	meta, _ := registry.Get("jsonDoc")
	want := map[string]bool{"ID": false, "Prefs": true, "Plain": false}
	for _, field := range meta.Fields {
		if field.JSON != want[field.Name] {
			t.Errorf("%s.JSON = %v, want %v", field.Name, field.JSON, want[field.Name])
		}
		if field.MaxLength != 0 {
			t.Errorf("%s.MaxLength = %d, want 0 (unbounded text)", field.Name, field.MaxLength)
		}
	}
}

type jsonOnInt struct {
	ID int64 `tango:"pk"`
	A  int   `tango:"json"`
}
type jsonWithValue struct {
	ID int64  `tango:"pk"`
	A  string `tango:"json=1"`
}
type jsonTwice struct {
	ID int64  `tango:"pk"`
	A  string `tango:"json,json"`
}
type jsonVarchar struct {
	ID int64  `tango:"pk"`
	A  string `tango:"json,varchar=10"`
}
type jsonText struct {
	ID int64  `tango:"pk"`
	A  string `tango:"json,text"`
}
type jsonPK struct {
	A string `tango:"pk,json"`
}
type jsonFK struct {
	ID int64  `tango:"pk"`
	A  string `tango:"json,fk=jsonDoc"`
}
type jsonUnique struct {
	ID int64  `tango:"pk"`
	A  string `tango:"json,unique"`
}
type jsonIndex struct {
	ID int64  `tango:"pk"`
	A  string `tango:"json,index"`
}

func TestJSONTagRefusalsFailAtRegistrationNamingModelAndField(t *testing.T) {
	tests := []struct {
		name  string
		model any
		want  string
	}{
		{"on an integer", jsonOnInt{}, "jsonOnInt.A"},
		{"with a value", jsonWithValue{}, "jsonWithValue.A"},
		{"declared twice", jsonTwice{}, "jsonTwice.A"},
		{"with varchar", jsonVarchar{}, "jsonVarchar.A"},
		{"with text", jsonText{}, "jsonText.A"},
		{"as the primary key", jsonPK{}, "jsonPK.A"},
		{"as a foreign key", jsonFK{}, "jsonFK.A"},
		{"unique", jsonUnique{}, "jsonUnique.A"},
		{"indexed", jsonIndex{}, "jsonIndex.A"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewRegistry().Register(tt.model)
			if !errors.Is(err, ErrInvalidFieldTag) {
				t.Fatalf("Register error = %v, want ErrInvalidFieldTag", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not name %q", err, tt.want)
			}
		})
	}
}
