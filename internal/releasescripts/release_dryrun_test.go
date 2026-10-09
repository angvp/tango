package releasescripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scriptRepo is a throwaway git repository holding a copy of one script, a
// stand-in scripts/check.sh and stand-ins for go and gh that log every call
// to calls.log. It returns the repository, the bin directory for PATH and
// the HEAD commit.
func scriptRepo(t *testing.T, script string, tags ...string) (repo, bin, head string) {
	t.Helper()
	dir := t.TempDir()
	repo = filepath.Join(dir, "repo")
	bin = filepath.Join(dir, "bin")
	for _, d := range []string{filepath.Join(repo, "scripts"), filepath.Join(repo, ".github", "workflows"), bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", script))
	if err != nil {
		t.Fatal(err)
	}
	write := func(path, content string, mode os.FileMode) {
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repo, "scripts", script), string(src), 0o755)
	write(filepath.Join(repo, "scripts", "check.sh"), "#!/bin/sh\necho \"check.sh\" >> \"$LOG\"\n", 0o755)
	write(filepath.Join(repo, ".github", "workflows", "release.yml"), "run: go run golang.org/x/exp/cmd/gorelease@v0.0.0-pinned -base=x\n", 0o644)
	write(filepath.Join(repo, "CHANGELOG.md"), "# Changelog\n", 0o644)
	// go: gorelease prints a report; releasecheck exits with $RELEASECHECK_EXIT.
	write(filepath.Join(bin, "go"), `#!/bin/sh
echo "go $*" >> "$LOG"
case "$*" in
*releasecheck*) exit "${RELEASECHECK_EXIT:-0}" ;;
*gorelease@*) echo "# summary"; echo "Suggested version: v0.3.0" ;;
esac
`, 0o755)
	write(filepath.Join(bin, "gh"), "#!/bin/sh\necho \"gh $*\" >> \"$LOG\"\necho fake-token\n", 0o755)

	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-q", "-m", "x")
	for _, tag := range tags {
		git("tag", "-a", tag, "-m", tag)
	}
	return repo, bin, git("rev-parse", "HEAD")
}

func runIn(t *testing.T, repo, bin, script string, env []string, args ...string) (out string, exit int, log string) {
	t.Helper()
	logPath := filepath.Join(filepath.Dir(bin), "calls.log")
	cmd := exec.Command("sh", append([]string{filepath.Join(repo, "scripts", script)}, args...)...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "LOG="+logPath)
	cmd.Env = append(cmd.Env, env...)
	combined, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		exit = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	logged, _ := os.ReadFile(logPath)
	return string(combined), exit, string(logged)
}

func TestDryRunRefusesAMissingOrMalformedVersion(t *testing.T) {
	for _, args := range [][]string{nil, {"0.3.0"}, {"v0.3"}, {"v0.3.0-rc.1"}} {
		repo, bin, _ := scriptRepo(t, "release-dryrun.sh")
		out, exit, log := runIn(t, repo, bin, "release-dryrun.sh", nil, args...)
		if exit == 0 {
			t.Fatalf("args %v: exit = 0, want a refusal:\n%s", args, out)
		}
		if log != "" {
			t.Fatalf("args %v: ran something before refusing:\n%s", args, log)
		}
	}
}

func TestDryRunRefusesADirtyTree(t *testing.T) {
	repo, bin, _ := scriptRepo(t, "release-dryrun.sh", "v0.2.0")
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, exit, log := runIn(t, repo, bin, "release-dryrun.sh", nil, "v0.3.0")
	if exit == 0 || !strings.Contains(out, "uncommitted") {
		t.Fatalf("exit=%d, want a refusal naming uncommitted changes:\n%s", exit, out)
	}
	if log != "" {
		t.Fatalf("ran something with a dirty tree:\n%s", log)
	}
}

func TestDryRunRunsTheChecksAndChangesNothing(t *testing.T) {
	repo, bin, head := scriptRepo(t, "release-dryrun.sh", "v0.2.0")
	out, exit, log := runIn(t, repo, bin, "release-dryrun.sh", nil, "v0.3.0")
	if exit != 0 {
		t.Fatalf("exit = %d:\n%s\n%s", exit, out, log)
	}
	for _, want := range []string{
		"check.sh",
		"gorelease@v0.0.0-pinned -base=v0.2.0",
		"releasecheck -dry-run",
		"-tag v0.3.0",
		"-commit " + head,
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("the calls do not include %q:\n%s", want, log)
		}
	}
	for _, forbidden := range []string{"-notes", "git tag", "git push", "gh release", "gh workflow run"} {
		if strings.Contains(log, forbidden) {
			t.Fatalf("a dry run must not call %q:\n%s", forbidden, log)
		}
	}
	status := exec.Command("git", "status", "--porcelain")
	status.Dir = repo
	if porcelain, _ := status.Output(); len(porcelain) != 0 {
		t.Fatalf("the dry run changed the working tree:\n%s", porcelain)
	}
}

func TestDryRunFailsWhenTheReleaseCheckRefuses(t *testing.T) {
	repo, bin, _ := scriptRepo(t, "release-dryrun.sh", "v0.2.0")
	out, exit, _ := runIn(t, repo, bin, "release-dryrun.sh", []string{"RELEASECHECK_EXIT=1"}, "v0.3.0")
	if exit == 0 {
		t.Fatalf("exit = 0 although the release check refused:\n%s", out)
	}
}
