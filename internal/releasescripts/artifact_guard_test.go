package releasescripts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// guardRepo is a throwaway git repository the artifact guard runs in.
type guardRepo struct {
	t   *testing.T
	dir string
}

func newGuardRepo(t *testing.T) *guardRepo {
	t.Helper()
	r := &guardRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	r.write("main.go", []byte("package main\n"), 0o644)
	r.commit("first")
	return r
}

func (r *guardRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *guardRepo) write(name string, data []byte, mode os.FileMode) {
	r.t.Helper()
	path := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		r.t.Fatal(err)
	}
}

func (r *guardRepo) commit(message string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", message)
}

// guard runs the script with base (empty for none) and returns its stderr and
// whether it passed.
func (r *guardRepo) guard(base string, env ...string) (string, bool) {
	r.t.Helper()
	script, err := filepath.Abs("../../scripts/check-artifacts.sh")
	if err != nil {
		r.t.Fatal(err)
	}
	args := []string{script}
	if base != "" {
		args = append(args, base)
	}
	cmd := exec.Command("sh", args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stderr.String(), err == nil
}

func machO() []byte { return append([]byte{0xcf, 0xfa, 0xed, 0xfe}, make([]byte, 64)...) }
func elf() []byte   { return append([]byte{0x7f, 'E', 'L', 'F'}, make([]byte, 64)...) }
func pe() []byte    { return append([]byte("MZ"), make([]byte, 64)...) }

func bigBinary(n int) []byte {
	data := bytes.Repeat([]byte{0x01, 0x00, 0x02}, n/3+1)
	return data[:n]
}

func TestGuardFailsForExecutablesInTheRange(t *testing.T) {
	for name, data := range map[string][]byte{"macho": machO(), "elf": elf(), "pe": pe()} {
		t.Run(name, func(t *testing.T) {
			r := newGuardRepo(t)
			base := r.git("rev-parse", "HEAD")
			r.write("tool/"+name, data, 0o755)
			r.commit("add a built tool")
			out, ok := r.guard(base)
			if ok || !strings.Contains(out, "tool/"+name) {
				t.Fatalf("passed=%v, want a failure naming the file:\n%s", ok, out)
			}
		})
	}
}

func TestGuardFailsForALargeBinaryButNotLargeText(t *testing.T) {
	r := newGuardRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.write("data/blob.dat", bigBinary(2<<20), 0o644)
	r.write("assets/bundle.js", bytes.Repeat([]byte("var a=1;\n"), 300000), 0o644)
	r.commit("add data")
	out, ok := r.guard(base)
	if ok || !strings.Contains(out, "data/blob.dat") || strings.Contains(out, "bundle.js") {
		t.Fatalf("passed=%v, want only blob.dat flagged:\n%s", ok, out)
	}
	if _, ok := r.guard(base, "GUARD_MAX_BYTES=4194304"); !ok {
		t.Fatal("a binary under the configured limit was flagged")
	}
}

func TestGuardPassesNormalChanges(t *testing.T) {
	r := newGuardRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.write("a/gen.go", bytes.Repeat([]byte("// generated\n"), 1000), 0o644)
	r.write("img/logo.png", append([]byte{0x89, 'P', 'N', 'G'}, make([]byte, 2000)...), 0o644)
	r.write("scripts/run.sh", []byte("#!/bin/sh\necho hi\n"), 0o755)
	r.commit("source")
	if out, ok := r.guard(base); !ok {
		t.Fatalf("a normal change failed:\n%s", out)
	}
}

func TestGuardHonoursTheAllowlist(t *testing.T) {
	r := newGuardRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.write("testdata/sample.bin", bigBinary(2<<20), 0o644)
	r.write("testdata/tool", elf(), 0o755)
	r.write("scripts/artifact-allowlist.txt", []byte("# fixtures\ntestdata/*  # inputs the parser tests need\n"), 0o644)
	r.commit("fixtures")
	if out, ok := r.guard(base); !ok {
		t.Fatalf("allowlisted files failed:\n%s", out)
	}
}

func TestGuardOnlyLooksAtTheRangeNotHistory(t *testing.T) {
	r := newGuardRepo(t)
	r.write("old/tool", machO(), 0o755)
	r.commit("a mistake that was published")
	r.git("rm", "-q", "old/tool")
	r.commit("remove it")
	base := r.git("rev-parse", "HEAD")
	r.write("new.go", []byte("package main\n"), 0o644)
	r.commit("source")
	if out, ok := r.guard(base); !ok {
		t.Fatalf("history was scanned:\n%s", out)
	}
}

func TestGuardCatchesUncommittedArtifacts(t *testing.T) {
	r := newGuardRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.write("untracked-tool", machO(), 0o755)
	if out, ok := r.guard(base); ok || !strings.Contains(out, "untracked-tool") {
		t.Fatalf("an untracked binary passed:\n%s", out)
	}
	if err := os.Remove(filepath.Join(r.dir, "untracked-tool")); err != nil {
		t.Fatal(err)
	}
	r.write("staged-tool", elf(), 0o755)
	r.git("add", "staged-tool")
	if out, ok := r.guard(base); ok || !strings.Contains(out, "staged-tool") {
		t.Fatalf("a staged binary passed:\n%s", out)
	}
}

func TestGuardWithoutAUsableBaseChecksTheLastCommit(t *testing.T) {
	r := newGuardRepo(t)
	r.write("tool", machO(), 0o755)
	r.commit("add a tool")
	for _, base := range []string{"", "0000000000000000000000000000000000000000", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"} {
		if out, ok := r.guard(base); ok || !strings.Contains(out, "tool") {
			t.Errorf("base %q: the last commit's binary passed:\n%s", base, out)
		}
	}
	single := t.TempDir()
	cmd := exec.Command("sh", "-c", "git init -q && printf 'x' > a.txt && git add . && git -c user.name=t -c user.email=t@e.test commit -qm one")
	cmd.Dir = single
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	script, _ := filepath.Abs("../../scripts/check-artifacts.sh")
	check := exec.Command("sh", script)
	check.Dir = single
	if out, err := check.CombinedOutput(); err != nil {
		t.Fatalf("a one-commit repository failed: %v\n%s", err, out)
	}
}

func TestGuardPassesOnThisRepositoryWorkingTree(t *testing.T) {
	script, err := filepath.Abs("../../scripts/check-artifacts.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script)
	cmd.Dir = "../.."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the repository's own change set fails the guard:\n%s", out)
	}
}
