package releasescripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fixtureChangelog = `# Changelog

## [Unreleased]

### Added

- A new thing.

## [0.2.0] - 2026-10-08

### Added

- Older.

[Unreleased]: https://github.com/angvp/tango/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/angvp/tango/compare/v0.1.0...v0.2.0
`

const fixtureVersions = `package migrationcompat

import (
	"github.com/angvp/tango/internal/migrationcompat/unreleased/migrations"
	v020 "github.com/angvp/tango/internal/migrationcompat/v0_2_0/migrations"
	"github.com/angvp/tango/migration"
)

type Generator struct {
	Dir        string
	Migrations []migration.Migration
}

var Generators = []Generator{
	{Dir: "v0_2_0", Migrations: v020.Migrations},
	{Dir: "unreleased", Migrations: migrations.Migrations},
}
`

// prepareRepo is a repository with a changelog, versions.go and a stand-in
// generate.sh that records its arguments in generate.log and writes a corpus.
func prepareRepo(t *testing.T, changelog string) (repo, bin string) {
	t.Helper()
	repo, bin, _ = scriptRepo(t, "prepare-release.sh", "v0.2.0")
	mc := filepath.Join(repo, "internal", "migrationcompat")
	if err := os.MkdirAll(filepath.Join(mc, "v0_2_0"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string, mode os.FileMode) {
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repo, "CHANGELOG.md"), changelog, 0o644)
	write(filepath.Join(mc, "versions.go"), fixtureVersions, 0o644)
	write(filepath.Join(mc, "v0_2_0", "models.json"), "{}", 0o644)
	write(filepath.Join(mc, "generate.sh"), `#!/bin/sh
echo "generate.sh $*" >> "$LOG"
dir="$(dirname "$0")/$(echo "$1" | tr . _)"
mkdir -p "$dir/migrations" && echo "{}" > "$dir/models.json"
`, 0o755)
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "fixtures"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo, bin
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPrepareReleaseDatesTheChangelogAndRegistersTheCorpus(t *testing.T) {
	repo, bin := prepareRepo(t, fixtureChangelog)
	out, exit, log := runIn(t, repo, bin, "prepare-release.sh", nil, "v0.3.0", "--date", "2026-11-02")
	if exit != 0 {
		t.Fatalf("exit = %d:\n%s", exit, out)
	}

	changelog := read(t, filepath.Join(repo, "CHANGELOG.md"))
	for _, want := range []string{
		"## [Unreleased]\n\n## [0.3.0] - 2026-11-02\n\n### Added\n\n- A new thing.",
		"[Unreleased]: https://github.com/angvp/tango/compare/v0.3.0...HEAD\n[0.3.0]: https://github.com/angvp/tango/compare/v0.2.0...v0.3.0\n[0.2.0]: ",
	} {
		if !strings.Contains(changelog, want) {
			t.Fatalf("CHANGELOG.md lacks %q:\n%s", want, changelog)
		}
	}
	if strings.Count(changelog, "## [Unreleased]") != 1 {
		t.Fatalf("want exactly one Unreleased section:\n%s", changelog)
	}

	if !strings.Contains(log, "generate.sh v0.3.0 local") {
		t.Fatalf("the corpus was not generated for v0.3.0 from this checkout:\n%s", log)
	}
	versions := read(t, filepath.Join(repo, "internal", "migrationcompat", "versions.go"))
	for _, want := range []string{
		`v030 "github.com/angvp/tango/internal/migrationcompat/v0_3_0/migrations"`,
		`{Dir: "v0_3_0", Migrations: v030.Migrations},`,
	} {
		if !strings.Contains(versions, want) {
			t.Fatalf("versions.go lacks %q:\n%s", want, versions)
		}
	}
	if strings.Index(versions, `Dir: "v0_3_0"`) > strings.Index(versions, `Dir: "unreleased"`) {
		t.Fatalf("the new generator should come before the unreleased one:\n%s", versions)
	}
	if fmt := exec.Command("gofmt", "-l", filepath.Join(repo, "internal", "migrationcompat", "versions.go")); true {
		if b, _ := fmt.Output(); len(b) != 0 {
			t.Fatalf("versions.go is not gofmt-clean:\n%s", versions)
		}
	}
}

func TestPrepareReleaseIsSafeToRunTwice(t *testing.T) {
	repo, bin := prepareRepo(t, fixtureChangelog)
	if _, exit, _ := runIn(t, repo, bin, "prepare-release.sh", nil, "v0.3.0", "--date", "2026-11-02"); exit != 0 {
		t.Fatal("first run failed")
	}
	before := read(t, filepath.Join(repo, "CHANGELOG.md")) + read(t, filepath.Join(repo, "internal", "migrationcompat", "versions.go"))

	// The first run's own edits are still uncommitted: they must not stop a rerun.
	out, exit, log := runIn(t, repo, bin, "prepare-release.sh", nil, "v0.3.0", "--date", "2030-01-01")
	if exit != 0 {
		t.Fatalf("rerun exit = %d:\n%s", exit, out)
	}
	after := read(t, filepath.Join(repo, "CHANGELOG.md")) + read(t, filepath.Join(repo, "internal", "migrationcompat", "versions.go"))
	if before != after {
		t.Fatalf("the rerun changed files:\n--- before\n%s\n--- after\n%s", before, after)
	}
	if !strings.Contains(out, "already dated") {
		t.Fatalf("the rerun should say the section is already dated:\n%s", out)
	}
	if strings.Count(log, "generate.sh") != 1 {
		t.Fatalf("the corpus was generated again:\n%s", log)
	}
}

func TestPrepareReleaseNeverRegeneratesAnExistingCorpus(t *testing.T) {
	repo, bin := prepareRepo(t, fixtureChangelog)
	existing := filepath.Join(repo, "internal", "migrationcompat", "v0_3_0")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(existing, "models.json")
	if err := os.WriteFile(marker, []byte(`{"hand":"written"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Commit it so the tree holds only what the script itself edits.
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "corpus"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	out, exit, log := runIn(t, repo, bin, "prepare-release.sh", nil, "v0.3.0", "--date", "2026-11-02")
	if exit != 0 {
		t.Fatalf("exit = %d:\n%s", exit, out)
	}
	if strings.Contains(log, "generate.sh") {
		t.Fatalf("an existing corpus was regenerated:\n%s", log)
	}
	if got := read(t, marker); got != `{"hand":"written"}` {
		t.Fatalf("the existing corpus was changed: %s", got)
	}
	if !strings.Contains(read(t, filepath.Join(repo, "internal", "migrationcompat", "versions.go")), `Dir: "v0_3_0"`) {
		t.Fatal("the existing corpus was not registered")
	}
}

func TestPrepareReleaseRefusesUnrelatedChanges(t *testing.T) {
	repo, bin := prepareRepo(t, fixtureChangelog)
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := read(t, filepath.Join(repo, "CHANGELOG.md"))
	out, exit, log := runIn(t, repo, bin, "prepare-release.sh", nil, "v0.3.0", "--date", "2026-11-02")
	if exit == 0 || !strings.Contains(out, "main.go") {
		t.Fatalf("exit=%d, want a refusal naming main.go:\n%s", exit, out)
	}
	if log != "" || read(t, filepath.Join(repo, "CHANGELOG.md")) != before {
		t.Fatalf("it edited or ran something despite refusing:\n%s", log)
	}
}

func TestPrepareReleaseRefusesNothingToRelease(t *testing.T) {
	repo, bin := prepareRepo(t, strings.Replace(fixtureChangelog, "### Added\n\n- A new thing.\n\n", "", 1))
	out, exit, _ := runIn(t, repo, bin, "prepare-release.sh", nil, "v0.3.0", "--date", "2026-11-02")
	if exit == 0 || !strings.Contains(out, "Unreleased") {
		t.Fatalf("exit=%d, want a refusal because Unreleased is empty:\n%s", exit, out)
	}
}

func TestPrepareReleaseRefusesBadArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"0.3.0"}, {"v0.3.0", "--date", "11/02/2026"}, {"v0.3.0", "--date"}, {"v0.3.0", "--bogus"}} {
		repo, bin := prepareRepo(t, fixtureChangelog)
		out, exit, log := runIn(t, repo, bin, "prepare-release.sh", nil, args...)
		if exit == 0 || log != "" {
			t.Fatalf("args %v: exit=%d log=%q, want a refusal that runs nothing:\n%s", args, exit, log, out)
		}
	}
}

func TestPrepareReleaseDatesWithTodayInUTCByDefaultAndNeverCommits(t *testing.T) {
	repo, bin := prepareRepo(t, fixtureChangelog)
	today := time.Now().UTC().Format("2006-01-02")
	if out, exit, _ := runIn(t, repo, bin, "prepare-release.sh", nil, "v0.3.0"); exit != 0 {
		t.Fatalf("exit = %d:\n%s", exit, out)
	}
	if got := read(t, filepath.Join(repo, "CHANGELOG.md")); !strings.Contains(got, "## [0.3.0] - "+today) {
		t.Fatalf("want the section dated %s:\n%s", today, got)
	}
	count := exec.Command("git", "rev-list", "--count", "HEAD")
	count.Dir = repo
	if b, _ := count.Output(); strings.TrimSpace(string(b)) != "2" {
		t.Fatalf("the script made a commit; commits = %s", b)
	}
	tags := exec.Command("git", "tag", "--list")
	tags.Dir = repo
	if b, _ := tags.Output(); strings.TrimSpace(string(b)) != "v0.2.0" {
		t.Fatalf("the script made a tag: %s", b)
	}
}
