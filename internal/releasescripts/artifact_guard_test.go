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

// withUpstream pushes the repository's current state to a bare clone and
// tracks it, so later commits are "ahead" of an upstream.
func (r *guardRepo) withUpstream() {
	r.t.Helper()
	bare := filepath.Join(r.t.TempDir(), "remote.git")
	r.git("init", "-q", "--bare", "-b", "main", bare)
	r.git("remote", "add", "origin", bare)
	r.git("push", "-q", "-u", "origin", "main")
}

func TestGuardWithoutABaseUsesTheMergeBaseWithTheUpstream(t *testing.T) {
	r := newGuardRepo(t)
	r.write("published/tool", machO(), 0o755)
	r.commit("an old mistake, already published")
	r.git("rm", "-q", "published/tool")
	r.commit("remove it")
	r.withUpstream()

	r.write("local/tool", elf(), 0o755)
	r.commit("add a tool two commits ago")
	r.write("later.go", []byte("package main\n"), 0o644)
	r.commit("source")
	r.write("more.go", []byte("package main\n"), 0o644)
	r.commit("more source")

	out, ok := r.guard("")
	if ok || !strings.Contains(out, "local/tool") {
		t.Fatalf("an executable added earlier in the unpushed range passed (ok=%v):\n%s", ok, out)
	}
	if strings.Contains(out, "published/tool") {
		t.Fatalf("a published commit was scanned:\n%s", out)
	}
}

func TestGuardWithAnUpstreamAndNothingAheadChecksOnlyTheWorkingTree(t *testing.T) {
	r := newGuardRepo(t)
	r.write("old/tool", machO(), 0o755)
	r.commit("published mistake")
	r.withUpstream()
	if out, ok := r.guard(""); !ok {
		t.Fatalf("published history was scanned:\n%s", out)
	}
}

func TestGuardExplicitBaseStillWinsOverTheUpstream(t *testing.T) {
	r := newGuardRepo(t)
	r.withUpstream()
	base := r.git("rev-parse", "HEAD")
	r.write("tool", machO(), 0o755)
	r.commit("add a tool")
	if out, ok := r.guard(base); ok || !strings.Contains(out, "tool") {
		t.Fatalf("passed=%v:\n%s", ok, out)
	}
	if out, ok := r.guard("", "GUARD_BASE="+base); ok || !strings.Contains(out, "tool") {
		t.Fatalf("GUARD_BASE ignored (ok=%v):\n%s", ok, out)
	}
}

func TestGuardFailsForFat64MachO(t *testing.T) {
	for name, magic := range map[string][]byte{
		"fat64":        {0xca, 0xfe, 0xba, 0xbf},
		"fat64-swap":   {0xbf, 0xba, 0xfe, 0xca},
		"fat":          {0xca, 0xfe, 0xba, 0xbe},
		"fat-swap":     {0xbe, 0xba, 0xfe, 0xca},
		"macho32":      {0xfe, 0xed, 0xfa, 0xce},
		"macho32-swap": {0xce, 0xfa, 0xed, 0xfe},
		"macho64":      {0xfe, 0xed, 0xfa, 0xcf},
	} {
		t.Run(name, func(t *testing.T) {
			r := newGuardRepo(t)
			base := r.git("rev-parse", "HEAD")
			r.write("bin/"+name, append(magic, make([]byte, 64)...), 0o755)
			r.commit("add")
			if out, ok := r.guard(base); ok || !strings.Contains(out, "bin/"+name) {
				t.Fatalf("passed=%v:\n%s", ok, out)
			}
		})
	}
}

func TestGuardRejectsAllowlistEntriesWithoutAReason(t *testing.T) {
	for name, line := range map[string]string{
		"no comment":   "testdata/sample.bin",
		"empty reason": "testdata/sample.bin  #",
		"blank reason": "testdata/sample.bin  #    ",
	} {
		t.Run(name, func(t *testing.T) {
			r := newGuardRepo(t)
			base := r.git("rev-parse", "HEAD")
			r.write("scripts/artifact-allowlist.txt", []byte("# header\n\n"+line+"\n"), 0o644)
			r.commit("allowlist")
			out, ok := r.guard(base)
			if ok || !strings.Contains(out, "allowlist") || !strings.Contains(out, "reason") {
				t.Fatalf("passed=%v, want a failure about the missing reason:\n%s", ok, out)
			}
		})
	}
	r := newGuardRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.write("scripts/artifact-allowlist.txt", []byte("# header\n\ntestdata/sample.bin # a parser input\n"), 0o644)
	r.commit("allowlist")
	if out, ok := r.guard(base); !ok {
		t.Fatalf("a documented entry failed:\n%s", out)
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
