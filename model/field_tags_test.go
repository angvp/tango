package model

import (
	"errors"
	"strings"
	"testing"
)

type bounded struct {
	ID    int64  `tango:"pk"`
	Title string `tango:"varchar=200"`
	Body  string `tango:"text"`
	Note  string
	Slug  string `tango:"unique,index,varchar=64"`
}

func TestVarcharTagSetsMaxLengthAndTextAndBareStayUnbounded(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(bounded{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	meta, _ := registry.Get("bounded")
	want := map[string]int{"ID": 0, "Title": 200, "Body": 0, "Note": 0, "Slug": 64}
	for _, field := range meta.Fields {
		if field.MaxLength != want[field.Name] {
			t.Errorf("%s.MaxLength = %d, want %d", field.Name, field.MaxLength, want[field.Name])
		}
	}
	for _, field := range meta.Fields {
		if field.Name == "Slug" && (!field.Unique || !field.Indexed) {
			t.Errorf("Slug lost its unique/index flags: %+v", field)
		}
	}
}

func TestATextTagIsOtherwiseIdenticalToABareString(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(bounded{}); err != nil {
		t.Fatal(err)
	}
	meta, _ := registry.Get("bounded")
	var body, note FieldMeta
	for _, field := range meta.Fields {
		switch field.Name {
		case "Body":
			body = field
		case "Note":
			note = field
		}
	}
	body.Name, note.Name = "", ""
	if body != note {
		t.Fatalf("a text-tagged field differs from a bare string: %+v vs %+v", body, note)
	}
}

type varcharNoValue struct {
	ID int64  `tango:"pk"`
	A  string `tango:"varchar"`
}
type varcharEmpty struct {
	ID int64  `tango:"pk"`
	A  string `tango:"varchar="`
}
type varcharZero struct {
	ID int64  `tango:"pk"`
	A  string `tango:"varchar=0"`
}
type varcharNegative struct {
	ID int64  `tango:"pk"`
	A  string `tango:"varchar=-5"`
}
type varcharNotANumber struct {
	ID int64  `tango:"pk"`
	A  string `tango:"varchar=many"`
}
type varcharTooLarge struct {
	ID int64  `tango:"pk"`
	A  string `tango:"varchar=10485761"`
}
type varcharAndText struct {
	ID int64  `tango:"pk"`
	A  string `tango:"varchar=10,text"`
}
type varcharTwice struct {
	ID int64  `tango:"pk"`
	A  string `tango:"varchar=10,varchar=20"`
}
type textTwice struct {
	ID int64  `tango:"pk"`
	A  string `tango:"text,text"`
}
type varcharOnInt struct {
	ID int64 `tango:"pk"`
	A  int   `tango:"varchar=10"`
}
type textOnBool struct {
	ID int64 `tango:"pk"`
	A  bool  `tango:"text"`
}
type textWithValue struct {
	ID int64  `tango:"pk"`
	A  string `tango:"text=5"`
}

func TestMalformedLengthTagsFailAtRegistrationNamingModelAndField(t *testing.T) {
	tests := []struct {
		name  string
		model any
		want  string
	}{
		{"varchar with no value", varcharNoValue{}, "varcharNoValue.A"},
		{"varchar with an empty value", varcharEmpty{}, "varcharEmpty.A"},
		{"varchar=0", varcharZero{}, "varcharZero.A"},
		{"a negative length", varcharNegative{}, "varcharNegative.A"},
		{"a non-numeric length", varcharNotANumber{}, "varcharNotANumber.A"},
		{"a length above PostgreSQL's limit", varcharTooLarge{}, "varcharTooLarge.A"},
		{"varchar and text together", varcharAndText{}, "varcharAndText.A"},
		{"varchar twice", varcharTwice{}, "varcharTwice.A"},
		{"text twice", textTwice{}, "textTwice.A"},
		{"varchar on an integer", varcharOnInt{}, "varcharOnInt.A"},
		{"text on a bool", textOnBool{}, "textOnBool.A"},
		{"text with a value", textWithValue{}, "textWithValue.A"},
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

type varcharAtLimit struct {
	ID int64  `tango:"pk"`
	A  string `tango:"varchar=10485760"`
}

func TestTheLargestPostgreSQLLengthIsAccepted(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(varcharAtLimit{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	meta, _ := registry.Get("varcharAtLimit")
	if got := meta.Fields[1].MaxLength; got != MaxVarcharLength {
		t.Fatalf("MaxLength = %d, want %d", got, MaxVarcharLength)
	}
}
