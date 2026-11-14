package migration

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ValidateModels checks what no single column can check alone: that a
// foreign key has the width of the primary key it references (MySQL refuses
// an INT column referencing a BIGINT key), and that an explicit index name
// collides with no other name in the schema, compared case-insensitively.
// It is exported for tango.Check and tango.DumpModels.
func ValidateModels(models []Model) error {
	problems := foreignKeyWidthProblems(models)
	problems = append(problems, indexNameProblems(models)...)
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return errors.New("tango migration: " + strings.Join(problems, "; "))
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
	owner    string
	explicit bool
}

func indexNameProblems(models []Model) []string {
	names := map[string][]schemaName{}
	claim := func(name, owner string, explicit bool) {
		key := strings.ToLower(name)
		names[key] = append(names[key], schemaName{owner: owner, explicit: explicit})
	}
	claim("tango_migrations", "tango's migration table", false)
	claim("PRIMARY", "MySQL's primary key index", false)
	for _, m := range models {
		claim(m.Name, "table "+m.Name, false)
		claim(m.Name+"_pkey", "the primary key of "+m.Name, false)
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
		if len(claims) < 2 || !anyExplicit(claims) {
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

func anyExplicit(claims []schemaName) bool {
	for _, c := range claims {
		if c.explicit {
			return true
		}
	}
	return false
}
