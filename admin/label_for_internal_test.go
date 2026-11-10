package admin

import "testing"

// The changelist cannot seed a dangling foreign key, because the migrations
// declare the reference, so the raw-key fallback for a missing row is tested
// here directly. The present key beside it shows the same map yields its label.
func TestLabelForFallsBackToTheRawKeyForAMissingRow(t *testing.T) {
	labels := map[any]string{int64(1): "Jane Doe"}

	if got := labelFor(labels, int64(1)); got != "Jane Doe" {
		t.Fatalf("labelFor(present key) = %q, want %q", got, "Jane Doe")
	}
	if got := labelFor(labels, int64(42)); got != "42" {
		t.Fatalf("labelFor(missing key) = %q, want the raw key %q", got, "42")
	}
	if got := labelFor(nil, int64(42)); got != "42" {
		t.Fatalf("labelFor(nil labels, failed read) = %q, want the raw key %q", got, "42")
	}
}
