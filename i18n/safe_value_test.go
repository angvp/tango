package i18n

import (
	"context"
	"strings"
	"testing"
)

func TestTNeverRendersAMalformedTranslation(t *testing.T) {
	cases := []struct {
		name     string
		fallback string
		value    string
		args     []any
		want     string
	}{
		{"same verb", "New %s", "Nuevo %s", []any{"Pet"}, "Nuevo Pet"},
		{"verb omitted", "New %s", "Nuevo", []any{"Pet"}, "New Pet"},
		{"verb added", "New %s", "Nuevo %s %s", []any{"Pet"}, "New Pet"},
		{"verb changed", "Got %d", "Tiene %s", []any{3}, "Got 3"},
		{"reordered arguments", "%s of %s", "%[2]s de %[1]s", []any{"A", "B"}, "B de A"},
		{"repeated argument", "%s and %s", "%[1]s y %[1]s", []any{"A", "B"}, "A and B"},
		{"explicit indexes in the fallback too", "%[2]s %[1]s", "%[2]s, %[1]s", []any{"A", "B"}, "B, A"},
		{"flags and width are free", "%s", "%-8s|", []any{"A"}, "A       |"},
		{"escaped percent is not a verb", "100%% of %s", "100%% de %s", []any{"x"}, "100% de x"},
		{"escaped percent dropped is still fine", "100%% of %s", "todo %s", []any{"x"}, "todo x"},
		{"star width consumes an argument", "%*d", "%d", []any{3, 7}, "  7"},
		{"unknown verb", "New %s", "Nuevo %z", []any{"Pet"}, "New Pet"},
		{"trailing percent", "New %s", "Nuevo %s %", []any{"Pet"}, "New Pet"},
		{"bad index", "New %s", "Nuevo %[x]s", []any{"Pet"}, "New Pet"},
		{"empty value", "New %s", "", []any{"Pet"}, "New Pet"},
		{"empty value without arguments", "Save", "", nil, "Save"},
		{"plain text", "Save", "Guardar", nil, "Guardar"},
		{"verbs but no arguments are compared too", "Hello %s", "Hola", nil, "Hello %s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCatalogsForTest()
			RegisterCatalog("es", map[string]string{"k": tc.value})
			got := T(WithLocale(context.Background(), "es"), "k", tc.fallback, tc.args...)
			if got != tc.want {
				t.Errorf("T = %q, want %q", got, tc.want)
			}
			if strings.Contains(got, "%!") {
				t.Errorf("T rendered malformed output %q", got)
			}
		})
	}
}

func TestTKeepsTranslationWhenTheFallbackItselfIsMalformed(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"k": "Hola"})
	// The fallback cannot be compared, so the translation is trusted: this is
	// today's behaviour for a host's own mistake.
	if got := T(WithLocale(context.Background(), "es"), "k", "Hello %", nil...); got != "Hola" {
		t.Errorf("T = %q, want the translation", got)
	}
}

func TestRejectedValueFallsThroughToTheBaseLanguage(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es-MX", map[string]string{"k": "Nuevo"})
	RegisterCatalog("es", map[string]string{"k": "Nuevo %s"})
	got := T(WithLocale(context.Background(), "es-MX"), "k", "New %s", "Pet")
	if got != "Nuevo Pet" {
		t.Errorf("T = %q, want the base language's usable value", got)
	}
}

func TestTVerbCheckAllocatesNothingOnceWarm(t *testing.T) {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"plain": "Guardar", "verb": "Nuevo %s"})
	ctx := WithLocale(context.Background(), "es")
	T(ctx, "plain", "Save")
	T(ctx, "verb", "New %s")
	for _, key := range []string{"plain", "verb"} {
		fallback := "Save"
		if key == "verb" {
			fallback = "New %s"
		}
		if allocs := testing.AllocsPerRun(100, func() { T(ctx, key, fallback) }); allocs != 0 {
			t.Errorf("T(%q) allocated %v times once warm, want 0", key, allocs)
		}
	}
}
