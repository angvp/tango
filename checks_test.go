package tango

import "testing"

type checksApp struct {
	name   string
	checks []AppCheck
}

func (a checksApp) Name() string { return a.name }

func (a checksApp) Register(*Registry) error { return nil }

func (a checksApp) Checks() []AppCheck { return a.checks }

func TestCheckerImplementationIsUsableAsChecker(t *testing.T) {
	var a App = checksApp{
		name: "widgets",
		checks: []AppCheck{
			{Description: "has at least one widget", Err: nil},
		},
	}

	checker, ok := a.(Checker)
	if !ok {
		t.Fatalf("checksApp does not satisfy Checker")
	}

	got := checker.Checks()
	if len(got) != 1 {
		t.Fatalf("Checks() returned %d checks, want 1", len(got))
	}
}

func TestCheckNilErrMeansPassed(t *testing.T) {
	passing := AppCheck{Description: "passing check", Err: nil}
	if passing.Err != nil {
		t.Fatalf("expected nil Err to represent a passed check")
	}
}
