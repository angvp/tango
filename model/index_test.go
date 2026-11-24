package model

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type indexAccount struct {
	ID int64 `tango:"pk"`
}

type indexEntry struct {
	ID        int64 `tango:"pk"`
	AccountID int64 `tango:"fk=indexAccount"`
	Key       string
	Kind      string
	CreatedAt time.Time
	Owner     *indexAccount `tango:"belongs_to=AccountID"`
	hidden    string
}

func TestRegisterRecordsCompositeDeclarationsInOrder(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(indexEntry{},
		Unique("AccountID", "Key").Named("uniq_entry_owner_key"),
		Index("AccountID", "CreatedAt"),
		Index("CreatedAt", "AccountID"),
	)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	meta, _ := registry.Get("indexEntry")
	want := []IndexMeta{
		{Fields: []string{"AccountID", "Key"}, Unique: true, Name: "uniq_entry_owner_key"},
		{Fields: []string{"AccountID", "CreatedAt"}},
		{Fields: []string{"CreatedAt", "AccountID"}},
	}
	if !reflect.DeepEqual(meta.Indexes, want) {
		t.Fatalf("Indexes = %#v, want %#v", meta.Indexes, want)
	}
}

func TestRegisterWithoutOptionsHasNoIndexes(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(indexEntry{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	meta, _ := registry.Get("indexEntry")
	if len(meta.Indexes) != 0 {
		t.Fatalf("Indexes = %#v, want none", meta.Indexes)
	}
}

func TestNamedReturnsACopy(t *testing.T) {
	base := Index("AccountID", "Key")
	named := base.Named("idx_custom")
	registry := NewRegistry()
	if err := registry.Register(indexEntry{}, base); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	meta, _ := registry.Get("indexEntry")
	if meta.Indexes[0].Name != "" {
		t.Fatalf("Name = %q, want the original option left unnamed", meta.Indexes[0].Name)
	}
	other := NewRegistry()
	if err := other.Register(indexEntry{}, named); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	meta, _ = other.Get("indexEntry")
	if meta.Indexes[0].Name != "idx_custom" {
		t.Fatalf("Name = %q, want idx_custom", meta.Indexes[0].Name)
	}
}

func TestOptionsDoNotShareTheCallersSlice(t *testing.T) {
	fields := []string{"AccountID", "Key"}
	option := Index(fields...)
	fields[0] = "Kind"
	registry := NewRegistry()
	if err := registry.Register(indexEntry{}, option); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	meta, _ := registry.Get("indexEntry")
	if meta.Indexes[0].Fields[0] != "AccountID" {
		t.Fatalf("Fields = %v, want the option unaffected by later changes to the caller's slice", meta.Indexes[0].Fields)
	}
}

func TestRegisterRefusesInvalidDeclarations(t *testing.T) {
	tests := []struct {
		name   string
		option RegisterOption
		want   string
	}{
		{"no fields", Index(), "at least two"},
		{"one field", Unique("Key"), "at least two"},
		{"repeated field", Index("Key", "Key"), `"Key" is repeated`},
		{"unknown field", Index("Key", "Nope"), `"Nope"`},
		{"unexported field", Index("Key", "hidden"), `"hidden"`},
		{"relation field", Index("Key", "Owner"), `"Owner" is a relation`},
		{"primary key", Unique("ID", "Key"), `"ID" is the primary key`},
		{"same tuple twice", nil, ""},
		{"empty name", Index("AccountID", "Key").Named(""), "name"},
		{"bad name", Index("AccountID", "Key").Named("bad-name"), "bad-name"},
		{"long name", Index("AccountID", "Key").Named(strings.Repeat("a", 64)), "63"},
	}
	for _, tc := range tests {
		if tc.option == nil {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			registry := NewRegistry()
			err := registry.Register(indexEntry{}, tc.option)
			if !errors.Is(err, ErrInvalidIndex) {
				t.Fatalf("error = %v, want ErrInvalidIndex", err)
			}
			if !strings.Contains(err.Error(), "indexEntry") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to name the model and contain %q", err, tc.want)
			}
			if _, ok := registry.Get("indexEntry"); ok {
				t.Fatalf("a refused declaration still registered the model")
			}
		})
	}
}

func TestRegisterRefusesTheSameOrderedTupleTwice(t *testing.T) {
	for name, options := range map[string][]RegisterOption{
		"two indexes": {Index("AccountID", "Key"), Index("AccountID", "Key").Named("other")},
		"two uniques": {Unique("AccountID", "Key"), Unique("AccountID", "Key")},
	} {
		t.Run(name, func(t *testing.T) {
			registry := NewRegistry()
			err := registry.Register(indexEntry{}, options...)
			if !errors.Is(err, ErrInvalidIndex) || !strings.Contains(err.Error(), "twice") {
				t.Fatalf("error = %v, want ErrInvalidIndex saying the tuple is declared twice", err)
			}
			if _, ok := registry.Get("indexEntry"); ok {
				t.Fatalf("a refused declaration still registered the model")
			}
		})
	}
}

func TestRegisterRefusesANilOption(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(indexEntry{}, nil)
	if !errors.Is(err, ErrInvalidIndex) {
		t.Fatalf("error = %v, want ErrInvalidIndex", err)
	}
}

func TestRegisterAcceptsPrefixesReversalsAndForeignKeys(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(indexEntry{},
		Index("AccountID", "Key", "Kind"),
		Index("AccountID", "Key"),
		Index("Key", "AccountID"),
		Unique("AccountID", "Key"),
	)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	meta, _ := registry.Get("indexEntry")
	if len(meta.Indexes) != 4 {
		t.Fatalf("Indexes = %#v, want all four declarations", meta.Indexes)
	}
}
