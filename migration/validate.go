package migration

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ErrConflictingMigrations is returned by Validate when the registry holds a
// migration identity that cannot be told apart: an exact (App, Name) pair
// registered more than once, or two distinct names in one app that share a
// migration number.
var ErrConflictingMigrations = errors.New("tango migration: conflicting migration names")

// migrationNumberPattern captures the leading four-digit number of a
// migration Name. It is the only copy of that rule; names without the prefix
// are not numbered and skip the number check.
var migrationNumberPattern = regexp.MustCompile(`^(\d{4})_`)

const (
	groupNumber = iota
	groupExactName
)

// conflictGroup is one line of Validate's error. For a number group, names
// holds the distinct names sharing the number. For an exact-name group, names
// holds the single colliding name and registrations counts its copies.
type conflictGroup struct {
	app           string
	key           string
	kind          int
	names         []string
	registrations int
}

// Validate rejects a registry whose migration identities conflict. Scope is
// per app: the same number in two apps is valid. The whole list is checked
// regardless of what is applied, so the result does not depend on database
// state. Every conflict group is reported in one error, sorted by app, then
// by number or exact name, with names sorted inside each group, so the same
// registry always yields the same text. Renumbering one migration is the
// remedy; there is no bypass.
func Validate(migrations []Migration) error {
	groups := append(exactNameGroups(migrations), numberGroups(migrations)...)
	if len(groups) == 0 {
		return nil
	}
	sortGroups(groups)

	lines := make([]string, len(groups))
	for i, g := range groups {
		lines[i] = g.describe()
	}
	return fmt.Errorf("%w:\n- %s", ErrConflictingMigrations, strings.Join(lines, "\n- "))
}

// exactNameGroups returns one group per (App, Name) registered more than once.
func exactNameGroups(migrations []Migration) []conflictGroup {
	counts := make(map[MigrationKey]int)
	for _, m := range migrations {
		counts[MigrationKey{App: m.App, Name: m.Name}]++
	}
	var groups []conflictGroup
	for key, count := range counts {
		if count > 1 {
			groups = append(groups, conflictGroup{
				app: key.App, key: key.Name, kind: groupExactName,
				names: []string{key.Name}, registrations: count,
			})
		}
	}
	return groups
}

// numberGroups returns one group per (App, number) shared by two or more
// distinct names. Repeated registrations of one name do not count.
func numberGroups(migrations []Migration) []conflictGroup {
	namesByNumber := distinctNamesByNumber(migrations)
	var groups []conflictGroup
	for key, names := range namesByNumber {
		if len(names) > 1 {
			groups = append(groups, conflictGroup{
				app: key.App, key: key.Name, kind: groupNumber,
				names: sortedKeys(names),
			})
		}
	}
	return groups
}

// distinctNamesByNumber collects, per (App, number), the set of distinct names
// carrying that number. Names without the number prefix are skipped.
func distinctNamesByNumber(migrations []Migration) map[MigrationKey]map[string]bool {
	namesByNumber := make(map[MigrationKey]map[string]bool)
	for _, m := range migrations {
		match := migrationNumberPattern.FindStringSubmatch(m.Name)
		if match == nil {
			continue
		}
		numberKey := MigrationKey{App: m.App, Name: match[1]}
		if namesByNumber[numberKey] == nil {
			namesByNumber[numberKey] = make(map[string]bool)
		}
		namesByNumber[numberKey][m.Name] = true
	}
	return namesByNumber
}

// sortGroups orders groups by app, then key, then kind, so the rendered error
// is independent of map iteration order.
func sortGroups(groups []conflictGroup) {
	sort.Slice(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if a.app != b.app {
			return a.app < b.app
		}
		if a.key != b.key {
			return a.key < b.key
		}
		return a.kind < b.kind
	})
}

func (g conflictGroup) describe() string {
	if g.kind == groupExactName {
		return fmt.Sprintf("app %q: name %q is registered %d times; remove the duplicate registration or rename one migration",
			g.app, g.key, g.registrations)
	}
	quoted := make([]string, len(g.names))
	for i, name := range g.names {
		quoted[i] = fmt.Sprintf("%q", name)
	}
	return fmt.Sprintf("app %q: number %s is shared by %s; renumber one migration",
		g.app, g.key, strings.Join(quoted, ", "))
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
