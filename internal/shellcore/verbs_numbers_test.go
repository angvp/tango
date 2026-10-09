package shellcore

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/testdb"
)

type Counter struct {
	ID    int64 `tango:"pk"`
	Level int8
	Small uint8
	Big   uint64
	Ratio float32
}

// bigNumber is not a uint64 to the interpreter's eyes, so it takes the number
// conversion path instead of being assigned as it is.
type bigNumber uint64

func counterVerbs(t *testing.T) *Verbs {
	t.Helper()
	shop := tango.NewApp("shop", func(r *tango.Registry) error { return r.Models().Register(Counter{}) })
	registry, err := tango.BuildRegistry(tango.Config{InstalledApps: []tango.App{shop}})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, registry.Models().All())
	store := db.NewStore(sqlDB, dialect)
	registry.SetStore(store)
	return NewVerbs(context.Background(), registry.Models(), store, false)
}

func TestNumbersAreStoredOnlyWhenTheyFitTheFieldExactly(t *testing.T) {
	v := counterVerbs(t)
	tests := []struct {
		name  string
		field string
		value any
		want  any // the stored value when the call must succeed
		err   string
	}{
		{"int8 minimum", "Level", -128, int64(-128), ""},
		{"int8 overflow", "Level", 128, nil, "does not fit int8"},
		{"whole float into int8", "Level", 3.0, int64(3), ""},
		{"fractional float into int8", "Level", 3.5, nil, "does not fit int8"},
		{"float too large to be exact", "Level", 1e300, nil, "does not fit int8"},
		{"uint8 maximum", "Small", 255, int64(255), ""},
		{"uint8 overflow", "Small", 256, nil, "does not fit uint8"},
		{"negative into unsigned", "Small", -1, nil, "does not fit uint8"},
		{"unsigned into unsigned", "Small", uint16(7), int64(7), ""},
		{"unsigned too large for a signed field", "Level", uint(300), nil, "does not fit int8"},
		{"int into uint64", "Big", 5, int64(5), ""},
		{"negative into uint64", "Big", int64(-1), nil, "does not fit uint64"},
		{"float beyond 2^53 into uint64", "Big", 1e19, nil, "does not fit uint64"},
		{"uint64 above MaxInt64 into uint64", "Big", bigNumber(math.MaxInt64) + 1, nil, "any"},
		{"uint64 above MaxInt64 into float32", "Ratio", bigNumber(math.MaxInt64) + 1, nil, "does not fit float32"},
		{"int into float32", "Ratio", 3, 3.0, ""},
		{"float32 overflow", "Ratio", 1e39, nil, "does not fit float32"},
		{"float into float32", "Ratio", 0.5, 0.5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, err := v.Count("shop.Counter")
			if err != nil {
				t.Fatal(err)
			}
			row, err := v.Create("shop.Counter", map[string]any{tt.field: tt.value})
			if tt.err != "" {
				// "any": accepted by the conversion, refused by the database
				// driver, whose message differs by dialect. Either way no row
				// is created.
				if err == nil || (tt.err != "any" && !strings.Contains(err.Error(), tt.err)) {
					t.Fatalf("Create(%s: %v) error = %v, want one containing %q", tt.field, tt.value, err, tt.err)
				}
				if after, err := v.Count("shop.Counter"); err != nil || after != before {
					t.Fatalf("rows after a refused Create = %d (%v), want %d", after, err, before)
				}
				return
			}
			if err != nil {
				t.Fatalf("Create(%s: %v): %v", tt.field, tt.value, err)
			}
			got, err := v.Get("shop.Counter", row["ID"])
			if err != nil {
				t.Fatal(err)
			}
			if !numbersEqual(got[tt.field], tt.want) {
				t.Fatalf("stored %s = %v (%T), want %v", tt.field, got[tt.field], got[tt.field], tt.want)
			}
		})
	}
}

// numbersEqual compares a stored number with the expected one whatever their
// Go integer or float types.
func numbersEqual(got, want any) bool {
	asFloat := func(x any) (float64, bool) {
		switch n := x.(type) {
		case int:
			return float64(n), true
		case int8:
			return float64(n), true
		case uint8:
			return float64(n), true
		case uint64:
			return float64(n), true
		case float32:
			return float64(n), true
		case int64:
			return float64(n), true
		case float64:
			return n, true
		}
		return 0, false
	}
	g, ok1 := asFloat(got)
	w, ok2 := asFloat(want)
	return ok1 && ok2 && g == w
}

func TestQueryOrderLimitAndOffsetTakeTheShapesTheInterpreterPasses(t *testing.T) {
	v := newVerbs(t, false)
	for _, title := range []string{"a", "b", "c", "d"} {
		if _, err := v.Create("blog.Post", map[string]any{"Title": title}); err != nil {
			t.Fatal(err)
		}
	}
	titles := func(query map[string]any) string {
		t.Helper()
		rows, err := v.List("blog.Post", query)
		if err != nil {
			t.Fatalf("List(%v): %v", query, err)
		}
		var out []string
		for _, row := range rows {
			out = append(out, row["Title"].(string))
		}
		return strings.Join(out, "")
	}
	tests := []struct {
		name  string
		query map[string]any
		want  string
	}{
		{"order as a []any of strings", map[string]any{"order": []any{"-Title"}}, "dcba"},
		{"order as a []string", map[string]any{"order": []string{"Title"}}, "abcd"},
		{"limit as int64", map[string]any{"order": []string{"ID"}, "limit": int64(2)}, "ab"},
		{"offset as uint", map[string]any{"order": []string{"ID"}, "offset": uint(3)}, "d"},
		{"limit and offset together", map[string]any{"order": []string{"ID"}, "limit": 2, "offset": 1}, "bc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := titles(tt.query); got != tt.want {
				t.Fatalf("titles = %q, want %q", got, tt.want)
			}
		})
	}

	bad := []struct {
		name  string
		query map[string]any
		want  string
	}{
		{"order list holds a number", map[string]any{"order": []any{"Title", 3}}, "must be field names, got int"},
		{"order is a number", map[string]any{"order": 3}, "must be a []string of field names, got int"},
		{"limit is a string", map[string]any{"limit": "3"}, `"limit" must be a whole number`},
		{"limit is fractional", map[string]any{"limit": 2.5}, `"limit" must be a whole number`},
		{"offset is negative", map[string]any{"offset": -1}, `"offset" must be a whole number`},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.List("blog.Post", tt.query)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestARejectedValueLeavesTheRowUnchanged(t *testing.T) {
	v := counterVerbs(t)
	row, err := v.Create("shop.Counter", map[string]any{"Level": 5, "Small": 9})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Update("shop.Counter", row["ID"], map[string]any{"Small": 300}); err == nil {
		t.Fatal("Update with 300 for a uint8 succeeded")
	}
	got, err := v.Get("shop.Counter", row["ID"])
	if err != nil {
		t.Fatal(err)
	}
	if !numbersEqual(got["Small"], 9) || !numbersEqual(got["Level"], 5) {
		t.Fatalf("row after a rejected update = %v, want Small 9 and Level 5", got)
	}
}
