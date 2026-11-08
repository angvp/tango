package migration

import "testing"

// A bare "0007" is unnumbered, so its two registrations form an exact-name
// group keyed "0007". "0007_a" and "0007_b" form a number group keyed "0007"
// in the same app. The two groups share (app, key) and differ only in kind.
func TestValidateOrdersNumberGroupBeforeExactNameGroupOfSameKey(t *testing.T) {
	const want = "tango migration: conflicting migration names:\n" +
		`- app "blog": number 0007 is shared by "0007_a", "0007_b"; renumber one migration` + "\n" +
		`- app "blog": name "0007" is registered 2 times; remove the duplicate registration or rename one migration`

	registry := []Migration{
		{App: "blog", Name: "0007"},
		{App: "blog", Name: "0007_b"},
		{App: "blog", Name: "0007"},
		{App: "blog", Name: "0007_a"},
	}
	err := Validate(registry)
	if err == nil || err.Error() != want {
		t.Fatalf("Validate error = %v, want exactly:\n%s", err, want)
	}
}
