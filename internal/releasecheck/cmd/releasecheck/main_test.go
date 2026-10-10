package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angvp/tango/internal/releasecheck"
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

// proxyWatcher is the sources a real check would ask, recording whether the
// module proxy was.
type proxyWatcher struct {
	releasecheck.Sources
	asked bool
}

func (w *proxyWatcher) PublishedCommit(context.Context, string) (string, error) {
	w.asked = true
	return "", nil
}

func TestACheckBeforeTaggingNeverAsksTheModuleProxy(t *testing.T) {
	// The proxy caches "unknown revision" for a while, which would delay the
	// real release; notYetTagged must answer without any lookup.
	watcher := &proxyWatcher{}
	got, err := notYetTagged{watcher}.PublishedCommit(context.Background(), "v9.9.9")
	if got != "" || err != nil {
		t.Fatalf("PublishedCommit = %q, %v; want empty and no error", got, err)
	}
	if watcher.asked {
		t.Fatal("the module proxy was asked about a tag that is not pushed")
	}
}

// readySources answers as a release whose commit is on main, whose CI is
// green and whose migration-compat corpus is in place.
type readySources struct {
	onMain bool
	jobs   []string
}

func (s readySources) OnMain(context.Context, string) (bool, error) { return s.onMain, nil }
func (s readySources) CIRuns(context.Context, string) ([]releasecheck.CIRun, error) {
	run := releasecheck.CIRun{Status: "completed", Conclusion: "success"}
	for _, name := range s.jobs {
		run.Jobs = append(run.Jobs, releasecheck.CIJob{Name: name, Status: "completed", Conclusion: "success"})
	}
	return []releasecheck.CIRun{run}, nil
}
func (readySources) PublishedCommit(context.Context, string) (string, error) { return "", nil }
func (readySources) CorpusRegistered(context.Context, string, string) (bool, bool, error) {
	return true, true, nil
}

const readyChangelog = "# Changelog\n\n## [Unreleased]\n\n## [0.4.0] - 2026-11-02\n\n### Added\n\n- Caching.\n"

func TestVerdictSaysSoAndWritesTheNotesForAReleasableTag(t *testing.T) {
	notes := filepath.Join(t.TempDir(), "notes.md")
	release := releasecheck.Release{Tag: "v0.4.0", Commit: "abc123", Changelog: []byte(readyChangelog)}
	var out strings.Builder
	if err := verdict(context.Background(), release, readySources{onMain: true, jobs: releasecheck.RequiredJobs}, notes, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "v0.4.0 at abc123 may be released.") {
		t.Fatalf("output = %q", out.String())
	}
	body, err := os.ReadFile(notes)
	if err != nil || !strings.Contains(string(body), "Caching.") || !strings.HasSuffix(string(body), "\n") {
		t.Fatalf("notes = %q, %v", body, err)
	}
}

func TestVerdictOnADryRunNamesWhatItDidNotCheck(t *testing.T) {
	release := releasecheck.Release{Tag: "v0.4.0", Commit: "abc123", Changelog: []byte(readyChangelog), DryRun: true}
	var out strings.Builder
	if err := verdict(context.Background(), release, readySources{onMain: false, jobs: releasecheck.RequiredJobs}, "", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "dry run: v0.4.0 at abc123 passes every check it makes.") || !strings.Contains(out.String(), "dry run: not checked:") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestVerdictRefusesAndWritesNothingWhenACheckFails(t *testing.T) {
	notes := filepath.Join(t.TempDir(), "notes.md")
	release := releasecheck.Release{Tag: "v0.4.0", Commit: "abc123", Changelog: []byte(readyChangelog)}
	var out strings.Builder
	err := verdict(context.Background(), release, readySources{onMain: false, jobs: releasecheck.RequiredJobs}, notes, &out)
	if err == nil || !strings.Contains(err.Error(), "not reachable from main") || out.Len() != 0 {
		t.Fatalf("err = %v, output %q", err, out.String())
	}
	if _, statErr := os.Stat(notes); statErr == nil {
		t.Fatal("release notes were written for a refused release")
	}
}

func TestVerdictReportsAnUnwritableNotesFile(t *testing.T) {
	release := releasecheck.Release{Tag: "v0.4.0", Commit: "abc123", Changelog: []byte(readyChangelog)}
	err := verdict(context.Background(), release, readySources{onMain: true, jobs: releasecheck.RequiredJobs}, filepath.Join(t.TempDir(), "no", "such", "dir", "notes.md"), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "write the release notes") {
		t.Fatalf("err = %v", err)
	}
}
