package i18ntest_test

import (
	"context"
	"testing"

	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/i18n/i18ntest"
)

func resolved(locale string) context.Context { return i18n.WithLocale(context.Background(), locale) }

func TestIsolateStartsCleanAndRestoresThePreviousState(t *testing.T) {
	// State the surrounding program set up before the test.
	i18n.RegisterCatalog("es", map[string]string{"outer": "exterior"})
	i18n.T(resolved("es"), "outer", "Outer")
	var outerMissing []string
	i18n.OnMissing(func(locale, key string) { outerMissing = append(outerMissing, key) })
	defer i18n.OnMissing(nil)

	t.Run("inside", func(t *testing.T) {
		i18ntest.Isolate(t)

		if got := i18n.T(resolved("es"), "outer", "Outer"); got != "Outer" {
			t.Errorf("the outer catalog leaked in: %q", got)
		}
		if keys := i18n.Keys(); len(keys) != 1 || keys[0].Key != "outer" {
			// The call above observed "outer" afresh; nothing from before survives.
			t.Errorf("Keys() = %v, want only what this test ran", keys)
		}
		if len(outerMissing) != 0 {
			t.Errorf("the outer OnMissing hook was called: %v", outerMissing)
		}

		i18n.RegisterCatalog("fr", map[string]string{"inner": "intérieur"})
		var innerMissing int
		i18n.OnMissing(func(string, string) { innerMissing++ })
		i18n.T(resolved("fr"), "absent", "Absent")
		i18n.T(resolved("fr"), "absent", "Absent")
		if innerMissing != 1 {
			t.Errorf("inner hook calls = %d, want 1", innerMissing)
		}
	})

	// Everything inside is gone and everything outside is back.
	if keys := i18n.Keys(); len(keys) != 1 || keys[0].Key != "outer" {
		t.Errorf("the outer observed keys were not restored: %v", keys)
	}
	if got := i18n.T(resolved("es"), "outer", "Outer"); got != "exterior" {
		t.Errorf("the outer catalog was not restored: %q", got)
	}
	if got := i18n.T(resolved("fr"), "inner", "Inner"); got != "Inner" {
		t.Errorf("the inner catalog leaked out: %q", got)
	}
	i18n.T(resolved("es"), "absent", "Absent")
	if len(outerMissing) != 1 || outerMissing[0] != "absent" {
		t.Errorf("the outer hook was not restored: %v", outerMissing)
	}
}

func TestIsolateGivesEachTestItsOwnOnceState(t *testing.T) {
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			i18ntest.Isolate(t)
			i18n.RegisterCatalog("es", map[string]string{"x": "y"})
			calls := 0
			i18n.OnMissing(func(string, string) { calls++ })
			i18n.T(resolved("es"), "same.key", "Same")
			if calls != 1 {
				t.Errorf("calls = %d, want the key reported once in each isolated test", calls)
			}
		})
	}
}

func TestIsolateResetsValidateState(t *testing.T) {
	t.Run("a test that leaves problems behind", func(t *testing.T) {
		i18ntest.Isolate(t)
		i18n.RegisterCatalog("es", map[string]string{"k": ""})
		i18n.T(resolved("es"), "k", "K")
		if len(i18n.Validate().Problems) == 0 {
			t.Fatal("expected a problem")
		}
	})
	if report := i18n.Validate(); len(report.Problems) != 0 {
		t.Errorf("problems leaked out of an isolated test: %+v", report.Problems)
	}
}
