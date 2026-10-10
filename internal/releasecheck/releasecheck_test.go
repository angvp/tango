package releasecheck

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const taggedCommit = "5e66893a20224b91c605aaa990f826c35b0eff37"

const changelog = `# Changelog

## [Unreleased]

## [0.1.1] - 2026-11-02

### Fixed

- A fix.

## [0.1.0] - 2026-10-20

### Added

- Everything.

## [0.0.9]

- Undated.
`

// fakeSources answers like git, GitHub Actions and the Go module proxy would.
type fakeSources struct {
	onMain    bool
	runs      []CIRun
	published string
	err       error
	// noCorpus and unregisteredCorpus make the version's migration-compat
	// fixtures missing, or present but not listed in Generators. The zero
	// value is a release whose corpus is in place.
	noCorpus           bool
	unregisteredCorpus bool
}

func (f fakeSources) OnMain(context.Context, string) (bool, error)    { return f.onMain, f.err }
func (f fakeSources) CIRuns(context.Context, string) ([]CIRun, error) { return f.runs, f.err }
func (f fakeSources) PublishedCommit(context.Context, string) (string, error) {
	return f.published, f.err
}

func (f fakeSources) CorpusRegistered(context.Context, string, string) (bool, bool, error) {
	return !f.noCorpus, !f.noCorpus && !f.unregisteredCorpus, f.err
}

func greenRun() CIRun {
	return CIRun{Status: "completed", Conclusion: "success", Jobs: []CIJob{
		{Name: "lint", Status: "completed", Conclusion: "success"},
		{Name: "test (sqlite)", Status: "completed", Conclusion: "success"},
		{Name: "test (postgres)", Status: "completed", Conclusion: "success"},
		{Name: "cache adapters", Status: "completed", Conclusion: "success"},
	}}
}

func withJob(run CIRun, name, status, conclusion string) CIRun {
	jobs := make([]CIJob, 0, len(run.Jobs))
	for _, job := range run.Jobs {
		if job.Name != name {
			jobs = append(jobs, job)
		}
	}
	if status != "" {
		jobs = append(jobs, CIJob{Name: name, Status: status, Conclusion: conclusion})
	}
	run.Jobs = jobs
	return run
}

const compatibleReport = `# github.com/angvp/tango/db
## compatible changes
DSN: added

# summary
Suggested version: v0.1.2
`

const incompatibleReport = `# github.com/angvp/tango
## incompatible changes
LoadDBDSNFromEnv: removed
## compatible changes
LoadDBConfigFromEnv: added

# summary
Suggested version: v0.2.0
`

func TestCheckPassesAReadyRelease(t *testing.T) {
	tests := []struct {
		name      string
		tag       string
		report    string
		wantNotes string
	}{
		{"patch with only compatible changes", "v0.1.1", compatibleReport, "### Fixed\n\n- A fix."},
		{"minor with incompatible changes", "v0.1.0", incompatibleReport, "### Added\n\n- Everything."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := Release{Tag: tt.tag, Commit: taggedCommit, Changelog: []byte(changelog), Gorelease: tt.report}
			sources := fakeSources{onMain: true, runs: []CIRun{greenRun()}, published: ""}
			notes, err := Check(context.Background(), release, sources)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if notes != tt.wantNotes {
				t.Fatalf("notes = %q, want %q", notes, tt.wantNotes)
			}
		})
	}
}

func TestCheckRefusesAReleaseThatIsNotReady(t *testing.T) {
	ready := func() (Release, fakeSources) {
		return Release{Tag: "v0.1.1", Commit: taggedCommit, Changelog: []byte(changelog), Gorelease: compatibleReport},
			fakeSources{onMain: true, runs: []CIRun{greenRun()}}
	}
	tests := []struct {
		name   string
		change func(*Release, *fakeSources)
		wants  []string
	}{
		{"not a release tag", func(r *Release, _ *fakeSources) { r.Tag = "v0.1" }, []string{`"v0.1"`, "vMAJOR.MINOR.PATCH"}},
		{"a prerelease tag", func(r *Release, _ *fakeSources) { r.Tag = "v0.1.1-rc.1" }, []string{"vMAJOR.MINOR.PATCH"}},
		{"taggedCommit not on main", func(_ *Release, s *fakeSources) { s.onMain = false }, []string{taggedCommit, "not reachable from main"}},
		{"no changelog section", func(r *Release, _ *fakeSources) { r.Tag = "v0.1.2" }, []string{"CHANGELOG.md", "no section", "0.1.2"}},
		{"undated changelog section", func(r *Release, _ *fakeSources) { r.Tag = "v0.0.9" }, []string{"0.0.9", "no date"}},
		{"no CI run for the commit", func(_ *Release, s *fakeSources) { s.runs = nil }, []string{"no successful CI run", taggedCommit}},
		{"CI still running", func(_ *Release, s *fakeSources) {
			s.runs = []CIRun{withJob(CIRun{Status: "in_progress", Jobs: greenRun().Jobs}, "test (postgres)", "in_progress", "")}
		}, []string{"still running", "re-run"}},
		{"a required job failed while another still runs", func(_ *Release, s *fakeSources) {
			run := withJob(CIRun{Status: "in_progress", Jobs: greenRun().Jobs}, "test (sqlite)", "in_progress", "")
			s.runs = []CIRun{withJob(run, "test (postgres)", "completed", "failure")}
		}, []string{"no successful CI run", "test (postgres)"}},
		{"a required job failed", func(_ *Release, s *fakeSources) {
			s.runs = []CIRun{withJob(greenRun(), "test (postgres)", "completed", "failure")}
		}, []string{"no successful CI run", "test (postgres)"}},
		{"the cache adapters job failed", func(_ *Release, s *fakeSources) {
			s.runs = []CIRun{withJob(greenRun(), "cache adapters", "completed", "failure")}
		}, []string{"no successful CI run", "cache adapters"}},
		{"a required job is missing", func(_ *Release, s *fakeSources) {
			s.runs = []CIRun{withJob(greenRun(), "lint", "", "")}
		}, []string{"no successful CI run", "lint"}},
		{"patch with incompatible changes", func(r *Release, _ *fakeSources) { r.Gorelease = incompatibleReport }, []string{"patch release", "incompatible", "LoadDBDSNFromEnv: removed"}},
		{"patch without a gorelease report", func(r *Release, _ *fakeSources) { r.Gorelease = "" }, []string{"gorelease"}},
		{"no migration-compat corpus", func(_ *Release, s *fakeSources) { s.noCorpus = true }, []string{"v0_1_1", "migrationcompat", "generate.sh v0.1.1 local"}},
		{"corpus not registered in Generators", func(_ *Release, s *fakeSources) { s.unregisteredCorpus = true }, []string{"v0_1_1", "Generators", "versions.go"}},
		{"tag already published for another commit", func(_ *Release, s *fakeSources) { s.published = "4fd666747631ada5854daa820c43fb03f80851f3" }, []string{"already", "4fd666747631ada5854daa820c43fb03f80851f3", "never move"}},
		{"a lookup fails", func(_ *Release, s *fakeSources) { s.err = errors.New("network down") }, []string{"network down"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release, sources := ready()
			tt.change(&release, &sources)
			_, err := Check(context.Background(), release, sources)
			if err == nil {
				t.Fatal("Check passed, want it refused")
			}
			for _, want := range tt.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// TestCheckReportsEveryProblemAtOnce saves a maintainer a retag per problem.
func TestCheckReportsEveryProblemAtOnce(t *testing.T) {
	release := Release{Tag: "v0.1.2", Commit: taggedCommit, Changelog: []byte(changelog), Gorelease: incompatibleReport}
	_, err := Check(context.Background(), release, fakeSources{onMain: false, noCorpus: true})
	if err == nil {
		t.Fatal("Check passed, want it refused")
	}
	for _, want := range []string{"not reachable from main", "no section", "no successful CI run", "incompatible", "v0_1_2"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

// TestCheckAcceptsAGreenRunAmongOthers covers a taggedCommit with a failed run and
// a later green re-run, or several workflows' runs.
func TestCheckAcceptsAGreenRunAmongOthers(t *testing.T) {
	release := Release{Tag: "v0.1.1", Commit: taggedCommit, Changelog: []byte(changelog), Gorelease: compatibleReport}
	runs := []CIRun{withJob(greenRun(), "lint", "completed", "failure"), greenRun()}
	if _, err := Check(context.Background(), release, fakeSources{onMain: true, runs: runs}); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheckAcceptsTheSameCommitAlreadyPublished(t *testing.T) {
	release := Release{Tag: "v0.1.1", Commit: taggedCommit, Changelog: []byte(changelog), Gorelease: compatibleReport}
	if _, err := Check(context.Background(), release, fakeSources{onMain: true, runs: []CIRun{greenRun()}, published: taggedCommit}); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

// proxyForbiddenSources fails the test if the module proxy is asked about a tag:
// asking makes the proxy fetch and cache the tag for good.
type proxyForbiddenSources struct {
	fakeSources
	t *testing.T
}

func (r proxyForbiddenSources) PublishedCommit(context.Context, string) (string, error) {
	r.t.Fatal("asked the module proxy about a release the other checks refuse")
	return "", nil
}

// TestCheckLeavesTheProxyAloneForARefusedRelease keeps a refused tag
// fixable: until the proxy has fetched it, the maintainer may still fix the
// taggedCommit and push the tag again.
func TestCheckLeavesTheProxyAloneForARefusedRelease(t *testing.T) {
	release := Release{Tag: "v0.1.2", Commit: taggedCommit, Changelog: []byte(changelog), Gorelease: compatibleReport}
	sources := proxyForbiddenSources{fakeSources: fakeSources{onMain: true, runs: []CIRun{greenRun()}}, t: t}
	if _, err := Check(context.Background(), release, sources); err == nil {
		t.Fatal("Check passed, want it refused for the missing changelog section")
	}
}

// neverAsked fails the test if a dry run asks the proxy about the tag, or
// whether the commit is on main: the two questions a rehearsal must skip.
type neverAsked struct {
	fakeSources
	t *testing.T
}

func (n neverAsked) OnMain(context.Context, string) (bool, error) {
	n.t.Fatal("a dry run asked whether the commit is on main")
	return false, nil
}

func (n neverAsked) PublishedCommit(context.Context, string) (string, error) {
	n.t.Fatal("a dry run asked the module proxy about the tag")
	return "", nil
}

func TestDryRunPassesAReadyCommitThatIsNotOnMainAndNeverAsksTheProxy(t *testing.T) {
	release := Release{Tag: "v0.1.1", Commit: taggedCommit, Changelog: []byte(changelog), Gorelease: compatibleReport, DryRun: true}
	notes, err := Check(context.Background(), release, neverAsked{fakeSources: fakeSources{runs: []CIRun{greenRun()}}, t: t})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if notes != "### Fixed\n\n- A fix." {
		t.Fatalf("notes = %q", notes)
	}
}

func TestDryRunStillRefusesEverythingElse(t *testing.T) {
	ready := Release{Tag: "v0.1.1", Commit: taggedCommit, Changelog: []byte(changelog), Gorelease: compatibleReport, DryRun: true}
	tests := []struct {
		name    string
		release Release
		sources fakeSources
		want    string
	}{
		{"no dated changelog section", Release{Tag: "v0.1.2", Commit: taggedCommit, Changelog: []byte(changelog), Gorelease: compatibleReport, DryRun: true}, fakeSources{runs: []CIRun{greenRun()}}, "no section"},
		{"CI is not green", ready, fakeSources{runs: nil}, "no successful CI run"},
		{"no corpus", ready, fakeSources{runs: []CIRun{greenRun()}, noCorpus: true}, "migration-compat corpus"},
		{"patch with incompatible changes", Release{Tag: "v0.1.1", Commit: taggedCommit, Changelog: []byte(changelog), Gorelease: incompatibleReport, DryRun: true}, fakeSources{runs: []CIRun{greenRun()}}, "incompatible"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Check(context.Background(), tt.release, neverAsked{fakeSources: tt.sources, t: t})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestSkippedNamesWhatADryRunDoesNotCheck(t *testing.T) {
	if got := Skipped(Release{}); len(got) != 0 {
		t.Fatalf("a real release skips %v, want nothing", got)
	}
	got := strings.Join(Skipped(Release{DryRun: true}), "|")
	for _, want := range []string{"on main", "module proxy"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Skipped = %q, want it to name %q", got, want)
		}
	}
}
