package migration

import (
	"fmt"
	"sort"
	"strings"

	"github.com/angvp/tango/model"
)

// ValidateModels checks what no single column can check alone: that a
// foreign key has the width of the primary key it references (MySQL refuses
// an INT column referencing a BIGINT key), and that an explicit index name
// collides with no other name in the schema, compared case-insensitively.
// Composite indexes are checked too: a derived name past 63 bytes, a tuple
// declared twice, an ordinary index repeating a unique tuple, and a name that
// collides with any other name in the schema. A problem with an index matches
// model.ErrInvalidIndex. It is exported for tango.Check and tango.DumpModels.
func ValidateModels(models []Model) error {
	problems := foreignKeyWidthProblems(models)
	indexProblems := append(indexNameProblems(models), compositeProblems(models)...)
	problems = append(problems, indexProblems...)
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return &validationError{message: "tango migration: " + strings.Join(problems, "; "), invalidIndex: len(indexProblems) > 0}
}

// validationError is ValidateModels' error. It keeps the message as it always
// was and matches model.ErrInvalidIndex when an index is among the problems.
type validationError struct {
	message      string
	invalidIndex bool
}

func (e *validationError) Error() string { return e.message }

func (e *validationError) Is(target error) bool {
	return e.invalidIndex && target == model.ErrInvalidIndex
}

func foreignKeyWidthProblems(models []Model) []string {
	keyWidth := make(map[string]int, len(models))
	for _, m := range models {
		for _, c := range m.Columns {
			if c.PrimaryKey && c.Type == "integer" {
				keyWidth[m.Name] = c.Width
			}
		}
	}
	var problems []string
	for _, m := range models {
		for _, c := range m.Columns {
			width, ok := keyWidth[c.References]
			if c.References == "" || c.Type != "integer" || !ok || width == c.Width {
				continue
			}
			problems = append(problems, fmt.Sprintf("%s.%s is %s but references %s's %s primary key; declare both int4 or neither",
				m.Name, c.Name, widthLabel(c.Width), c.References, widthLabel(width)))
		}
	}
	return problems
}

// schemaName is one name in the schema's index namespace, and whether the
// model declared it explicitly.
type schemaName struct {
	owner     string
	explicit  bool
	composite bool
}

func indexNameProblems(models []Model) []string {
	names := map[string][]schemaName{}
	claim := func(name, owner string, explicit bool, composite ...bool) {
		key := strings.ToLower(name)
		names[key] = append(names[key], schemaName{owner: owner, explicit: explicit, composite: len(composite) > 0 && composite[0]})
	}
	claim("tango_migrations", "tango's migration table", false)
	claim("PRIMARY", "MySQL's primary key index", false)
	for _, m := range models {
		claim(m.Name, "table "+m.Name, false)
		claim(m.Name+"_pkey", "the primary key of "+m.Name, false)
		for _, ix := range m.Indexes {
			kind := "the index"
			if ix.Unique {
				kind = "the unique index"
			}
			claim(compositeIndexName(m.Name, ix), kind+" "+compositeOwner(m.Name, ix), ix.Name != "", true)
		}
		for _, c := range m.Columns {
			owner := m.Name + "." + c.Name
			if c.PrimaryKey && c.Type == "integer" {
				claim(m.Name+"_"+c.Name+"_seq", "the sequence of "+owner, false)
			}
			if c.Unique {
				claim(columnUniqueName(m.Name, c), "the unique index of "+owner, c.UniqueName != "")
			}
			if c.Indexed {
				claim(columnIndexName(m.Name, c), "the index of "+owner, c.IndexName != "")
			}
		}
	}
	var problems []string
	for key, claims := range names {
		if len(claims) < 2 || !anyExplicitOrComposite(claims) {
			continue
		}
		owners := make([]string, len(claims))
		for i, c := range claims {
			owners[i] = c.owner
		}
		problems = append(problems, fmt.Sprintf("index name %q is used by %s", key, strings.Join(owners, " and ")))
	}
	return problems
}

func anyExplicitOrComposite(claims []schemaName) bool {
	for _, c := range claims {
		if c.explicit || c.composite {
			return true
		}
	}
	return false
}
