package model

import (
	"errors"
	"testing"
)

type fkIndexDefault struct {
	ID    int64 `tango:"pk"`
	Owner int64 `tango:"fk=Author"`
}

type fkIndexOptOut struct {
	ID    int64 `tango:"pk"`
	Owner int64 `tango:"fk=Author,noindex"`
}

type fkIndexUnique struct {
	ID    int64 `tango:"pk"`
	Owner int64 `tango:"fk=Author,unique"`
}

type fkIndexConflict struct {
	ID    int64 `tango:"pk"`
	Owner int64 `tango:"fk=Author,index,noindex"`
}

type noindexWithoutFK struct {
	ID   int64  `tango:"pk"`
	Name string `tango:"noindex"`
}

func fieldIndexed(t *testing.T, model any, name string) (indexed bool, err error) {
	t.Helper()
	registry := NewRegistry()
	if err := registry.Register(model); err != nil {
		return false, err
	}
	for _, meta := range registry.All() {
		for _, f := range meta.Fields {
			if f.Name == name {
				return f.Indexed, nil
			}
		}
	}
	t.Fatalf("no field %q", name)
	return false, nil
}

func TestForeignKeyFieldIsIndexedByDefault(t *testing.T) {
	got, err := fieldIndexed(t, fkIndexDefault{}, "Owner")
	if err != nil || !got {
		t.Fatalf("Indexed = %v, err = %v; want a foreign key indexed by default", got, err)
	}
}

func TestNoindexOptsAForeignKeyOutOfTheDefaultIndex(t *testing.T) {
	got, err := fieldIndexed(t, fkIndexOptOut{}, "Owner")
	if err != nil || got {
		t.Fatalf("Indexed = %v, err = %v; want no index", got, err)
	}
}

func TestUniqueForeignKeyGetsNoSecondIndex(t *testing.T) {
	got, err := fieldIndexed(t, fkIndexUnique{}, "Owner")
	if err != nil || got {
		t.Fatalf("Indexed = %v, err = %v; the unique constraint already indexes the column", got, err)
	}
}

func TestIndexAndNoindexTogetherAreRefused(t *testing.T) {
	_, err := fieldIndexed(t, fkIndexConflict{}, "Owner")
	if !errors.Is(err, ErrInvalidFieldTag) {
		t.Fatalf("err = %v, want ErrInvalidFieldTag", err)
	}
}

func TestNoindexOnAFieldThatIsNotAForeignKeyIsRefused(t *testing.T) {
	_, err := fieldIndexed(t, noindexWithoutFK{}, "Name")
	if !errors.Is(err, ErrInvalidFieldTag) {
		t.Fatalf("err = %v, want ErrInvalidFieldTag", err)
	}
}
