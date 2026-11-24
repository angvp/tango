package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrInvalidIndex is wrapped by every refusal of a composite unique or index
// declaration: at registration for what concerns one model, and by the
// migration package's schema validation for what concerns the whole schema.
var ErrInvalidIndex = errors.New("tango: invalid index declaration")

// IndexMeta describes one composite unique constraint or ordinary index
// declared with Unique or Index when a model was registered.
type IndexMeta struct {
	// Fields are the Go field names of the tuple, in declaration order; the
	// order is significant.
	Fields []string
	// Unique is true for a unique constraint and false for an ordinary index.
	Unique bool
	// Name is the explicit name from Named; "" means the derived name.
	Name string
}

// RegisterOption changes how Registry.Register records a model. The options
// are Index and Unique; the interface is closed to this package.
type RegisterOption interface {
	applyRegister(*registration)
}

type registration struct {
	indexes []IndexOption
}

// IndexOption declares a composite unique constraint or index over two or more
// fields of a model. Build one with Index or Unique and pass it to Register.
type IndexOption struct {
	meta IndexMeta
	// named is true when Named was called, so an empty name is refused
	// instead of read as "derived".
	named bool
}

// Index declares an ordinary index over fields, in this order, named by Go
// field name. At least two distinct persisted non-primary-key fields are
// required; Register refuses anything else with ErrInvalidIndex.
func Index(fields ...string) IndexOption {
	return IndexOption{meta: IndexMeta{Fields: slices.Clone(fields)}}
}

// Unique declares a unique constraint over fields, in this order. A row with
// a NULL in any of the fields is never a duplicate: every supported database
// accepts several of them.
func Unique(fields ...string) IndexOption {
	return IndexOption{meta: IndexMeta{Fields: slices.Clone(fields), Unique: true}}
}

// Named returns a copy of the option with an explicit index name, in place of
// the derived uniq_<table>_<columns> or idx_<table>_<columns>.
func (o IndexOption) Named(name string) IndexOption {
	copied := o
	copied.meta.Fields = slices.Clone(o.meta.Fields)
	copied.meta.Name = name
	copied.named = true
	return copied
}

func (o IndexOption) applyRegister(r *registration) {
	r.indexes = append(r.indexes, o)
}

// indexesFor checks the declarations in options against meta and returns them
// in declaration order. It registers nothing.
func indexesFor(meta ModelMeta, options []IndexOption) ([]IndexMeta, error) {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s: %s", ErrInvalidIndex, meta.Name, fmt.Sprintf(format, args...))
	}
	fields := make(map[string]FieldMeta, len(meta.Fields))
	for _, f := range meta.Fields {
		fields[f.Name] = f
	}
	relations := make(map[string]bool, len(meta.Relations))
	for _, r := range meta.Relations {
		relations[r.Name] = true
	}
	checked := make([]IndexMeta, 0, len(options))
	for _, option := range options {
		ix := option.meta
		label := "(" + strings.Join(ix.Fields, ", ") + ")"
		if len(ix.Fields) < 2 {
			return nil, fail("%s needs at least two fields", label)
		}
		seen := make(map[string]bool, len(ix.Fields))
		for _, name := range ix.Fields {
			switch f, known := fields[name]; {
			case seen[name]:
				return nil, fail("%s: field %q is repeated", label, name)
			case relations[name]:
				return nil, fail("%s: field %q is a relation, not a column", label, name)
			case !known:
				return nil, fail("%s: unknown field %q", label, name)
			case f.PrimaryKey:
				return nil, fail("%s: field %q is the primary key, which is already unique", label, name)
			}
			seen[name] = true
		}
		if option.named {
			if err := checkIndexName(ix.Name); err != nil {
				return nil, fail("%s: %v", label, err)
			}
		}
		for _, other := range checked {
			if other.Unique == ix.Unique && slices.Equal(other.Fields, ix.Fields) {
				return nil, fail("%s is declared twice", label)
			}
		}
		ix.Fields = slices.Clone(ix.Fields)
		checked = append(checked, ix)
	}
	return checked, nil
}
