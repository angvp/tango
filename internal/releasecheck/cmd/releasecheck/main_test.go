package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRefusesBeforeAskingAnyoneAnything(t *testing.T) {
	dir := t.TempDir()
	changelog := filepath.Join(dir, "CHANGELOG.md")
	if err := os.WriteFile(changelog, []byte("# Changelog\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir) // not a git checkout, so no tag resolves here
	tests := []struct {
		name                        string
		tag, commit, log, gorelease string
		want                        string
	}{
		{"no tag", "", "", changelog, "", "-tag is required"},
		{"a tag that does not exist and no commit", "v9.9.9", "", changelog, "", "tag v9.9.9 doesn't exist: pass -commit"},
		{"an unreadable changelog", "v9.9.9", "abc123", filepath.Join(dir, "missing.md"), "", "read the changelog"},
		{"an unreadable gorelease report", "v9.9.9", "abc123", changelog, filepath.Join(dir, "missing.txt"), "read the gorelease report"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.tag, tt.commit, tt.log, tt.gorelease, "", "angvp/tango", false)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("run error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestEnvOrFallsBackOnlyWhenTheVariableIsEmpty(t *testing.T) {
	t.Setenv("RELEASECHECK_TEST_REPO", "")
	if got := envOr("RELEASECHECK_TEST_REPO", "angvp/tango"); got != "angvp/tango" {
		t.Errorf("empty variable: %q, want the fallback", got)
	}
	t.Setenv("RELEASECHECK_TEST_REPO", "someone/else")
	if got := envOr("RELEASECHECK_TEST_REPO", "angvp/tango"); got != "someone/else" {
		t.Errorf("set variable: %q, want its value", got)
	}
}

func TestACheckBeforeTaggingNeverAsksTheModuleProxy(t *testing.T) {
	// The proxy caches "unknown revision" for a while, which would delay the
	// real release; notYetTagged must answer without any lookup.
	got, err := notYetTagged{}.PublishedCommit(context.Background(), "v9.9.9")
	if got != "" || err != nil {
		t.Fatalf("PublishedCommit = %q, %v; want empty and no error", got, err)
	}
}
