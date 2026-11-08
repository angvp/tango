package migration

import (
	"errors"
	"testing"
)

func TestValidateAcceptsRegistriesWithoutConflicts(t *testing.T) {
	tests := []struct {
		name       string
		migrations []Migration
	}{
		{name: "empty registry", migrations: nil},
		{name: "single migration", migrations: []Migration{
			{App: "blog", Name: "0001_initial"},
		}},
		{name: "same number in two different apps", migrations: []Migration{
			{App: "blog", Name: "0005_add_email"},
			{App: "shop", Name: "0005_add_phone"},
		}},
		{name: "distinct unnumbered names in one app", migrations: []Migration{
			{App: "blog", Name: "add_email"},
			{App: "blog", Name: "add_phone"},
		}},
		{name: "names that only look numbered pass the number check", migrations: []Migration{
			{App: "blog", Name: "12_a"},
			{App: "blog", Name: "12_b"},
			{App: "blog", Name: "12345_c"},
			{App: "blog", Name: "12345_d"},
		}},
		{name: "distinct numbers in one app", migrations: []Migration{
			{App: "blog", Name: "0001_initial"},
			{App: "blog", Name: "0002_posts"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate(tt.migrations); err != nil {
				t.Fatalf("Validate returned error: %v", err)
			}
		})
	}
}

func TestValidateRejectsExactDuplicateMigration(t *testing.T) {
	err := Validate([]Migration{
		{App: "blog", Name: "0001_initial"},
		{App: "blog", Name: "0001_initial"},
	})
	const want = "tango migration: conflicting migration names:\n" +
		`- app "blog": name "0001_initial" is registered 2 times; remove the duplicate registration or rename one migration`
	if err == nil || err.Error() != want {
		t.Fatalf("Validate error = %v, want exactly:\n%s", err, want)
	}
}

func TestValidateRejectsNumberCollisionWithDifferentSuffix(t *testing.T) {
	err := Validate([]Migration{
		{App: "blog", Name: "0005_add_phone"},
		{App: "blog", Name: "0005_add_email"},
	})
	const want = "tango migration: conflicting migration names:\n" +
		`- app "blog": number 0005 is shared by "0005_add_email", "0005_add_phone"; renumber one migration`
	if err == nil || err.Error() != want {
		t.Fatalf("Validate error = %v, want exactly:\n%s", err, want)
	}
}

func TestValidateRejectsExactDuplicateOfUnnumberedName(t *testing.T) {
	err := Validate([]Migration{
		{App: "blog", Name: "add_email"},
		{App: "blog", Name: "add_email"},
	})
	const want = "tango migration: conflicting migration names:\n" +
		`- app "blog": name "add_email" is registered 2 times; remove the duplicate registration or rename one migration`
	if err == nil || err.Error() != want {
		t.Fatalf("Validate error = %v, want exactly:\n%s", err, want)
	}
}

// An exact duplicate of a numbered name is reported only as an exact-name
// group. Number groups count distinct names, so two registrations of
// 0005_add_email do not make a number group on their own.
func TestValidateReportsExactDuplicateOfNumberedNameOnlyAsNameGroup(t *testing.T) {
	err := Validate([]Migration{
		{App: "blog", Name: "0005_add_email"},
		{App: "blog", Name: "0005_add_email"},
	})
	const want = "tango migration: conflicting migration names:\n" +
		`- app "blog": name "0005_add_email" is registered 2 times; remove the duplicate registration or rename one migration`
	if err == nil || err.Error() != want {
		t.Fatalf("Validate error = %v, want exactly:\n%s", err, want)
	}
}

func TestValidateReportsEveryConflictGroupInOneDeterministicError(t *testing.T) {
	const want = "tango migration: conflicting migration names:\n" +
		`- app "blog": number 0005 is shared by "0005_add_email", "0005_add_phone"; renumber one migration` + "\n" +
		`- app "blog": name "0007_x" is registered 2 times; remove the duplicate registration or rename one migration` + "\n" +
		`- app "shop": number 0005 is shared by "0005_cart", "0005_cart_items"; renumber one migration`

	registry := []Migration{
		{App: "shop", Name: "0005_cart_items"},
		{App: "blog", Name: "0007_x"},
		{App: "blog", Name: "0005_add_phone"},
		{App: "shop", Name: "0005_cart"},
		{App: "blog", Name: "0007_x"},
		{App: "blog", Name: "0005_add_email"},
	}
	orders := map[string][]Migration{
		"as given": registry,
		"reversed": reversedMigrations(registry),
		"rotated":  append(append([]Migration(nil), registry[3:]...), registry[:3]...),
	}
	for label, order := range orders {
		err := Validate(order)
		if err == nil || err.Error() != want {
			t.Fatalf("%s order: Validate error = %v, want exactly:\n%s", label, err, want)
		}
	}
}

func TestValidateWrapsDuplicateMigrationSentinelForEveryFailure(t *testing.T) {
	failures := map[string][]Migration{
		"exact duplicate":        {{App: "blog", Name: "0001_initial"}, {App: "blog", Name: "0001_initial"}},
		"number collision":       {{App: "blog", Name: "0005_a"}, {App: "blog", Name: "0005_b"}},
		"unnumbered duplicate":   {{App: "blog", Name: "x"}, {App: "blog", Name: "x"}},
		"several groups at once": {{App: "blog", Name: "0005_a"}, {App: "blog", Name: "0005_b"}, {App: "shop", Name: "x"}, {App: "shop", Name: "x"}},
	}
	for label, registry := range failures {
		err := Validate(registry)
		if !errors.Is(err, ErrConflictingMigrations) {
			t.Fatalf("%s: errors.Is(err, ErrConflictingMigrations) = false for %v", label, err)
		}
	}
}

func reversedMigrations(in []Migration) []Migration {
	out := make([]Migration, len(in))
	for i, m := range in {
		out[len(in)-1-i] = m
	}
	return out
}
