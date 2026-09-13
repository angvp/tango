package tango_test

// This file enforces Milestone 13's exit criterion: a host project can
// install a reusable tanGO app — distributed as its own separate Go module
// — into its own InstalledApps and get that app's model, routes, admin
// registration, and checks, with no internals copied. examples_test.go
// already proves both examples/reusable-greetings (the reusable app's own
// module) and examples/reusable-greetings-host (the host importing it) each
// build and pass their own -check independently; this test additionally
// proves the host's registry actually contains what the imported app
// contributed, not just that both modules happen to compile.

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestReusableAppInstallsIntoHostProject(t *testing.T) {
	const hostDir = "examples/reusable-greetings-host"

	cmd := exec.Command("go", "run", ".", "-tango-dump-models")
	cmd.Dir = hostDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . -tango-dump-models in %s failed: %v\n%s", hostDir, err, out)
	}

	var models []struct {
		App  string
		Name string
	}
	if err := json.Unmarshal(out, &models); err != nil {
		t.Fatalf("could not parse -tango-dump-models output as JSON: %v\n%s", err, out)
	}

	var found bool
	for _, m := range models {
		if m.App == "greetings" && m.Name == "greeting" {
			found = true
		}
	}
	if !found {
		t.Fatalf("host's dumped models do not include the reusable greetings app's Greeting model — the imported app's model registration did not take effect in the host:\n%s", out)
	}

	// The host's -check exercises route compilation (the greetings app's
	// namespaced route), admin registration (registry.Admin().Register),
	// and the app's Checker implementation all in one pass — a failure in
	// any of them fails -check.
	checkCmd := exec.Command("go", "run", ".", "-check")
	checkCmd.Dir = hostDir
	checkOut, err := checkCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . -check in %s failed: %v\n%s", hostDir, err, checkOut)
	}
	if !strings.Contains(string(checkOut), "check passed") {
		t.Fatalf("go run . -check in %s did not report success:\n%s", hostDir, checkOut)
	}
}
