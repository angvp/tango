package releasecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// earlierCommit is main's commit before the tagged one: its green CI run
// proves nothing about the tagged commit, so CIRuns must not return it.
const earlierCommit = "429ef1e0000000000000000000000000000000000"

// TestLiveReadsGitHubAndTheModuleProxy checks that CIRuns returns only the CI
// workflow's runs for exactly the tagged commit.
func TestLiveReadsGitHubAndTheModuleProxy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/angvp/tango/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("head_sha") != taggedCommit || r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"workflow_runs":[
			{"id":1,"path":".github/workflows/ci.yml","head_sha":"` + taggedCommit + `","status":"completed","conclusion":"success"},
			{"id":2,"path":".github/workflows/release.yml","head_sha":"` + taggedCommit + `","status":"completed","conclusion":"success"},
			{"id":3,"path":".github/workflows/ci.yml","head_sha":"` + earlierCommit + `","status":"completed","conclusion":"success"}]}`))
	})
	mux.HandleFunc("GET /repos/angvp/tango/actions/runs/1/jobs", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jobs":[{"name":"lint","status":"completed","conclusion":"success","id":9}]}`))
	})
	mux.HandleFunc("GET /github.com/angvp/tango/@v/v0.0.2.info", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Version":"v0.0.2","Origin":{"VCS":"git","Hash":"4fd666747631ada5854daa820c43fb03f80851f3"}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	live := Live{Repo: "angvp/tango", Module: "github.com/angvp/tango", Token: "token", API: server.URL, Proxy: server.URL}
	ctx := context.Background()

	runs, err := live.CIRuns(ctx, taggedCommit)
	if err != nil {
		t.Fatalf("CIRuns: %v", err)
	}
	want := []CIRun{{Status: "completed", Conclusion: "success", Jobs: []CIJob{{Name: "lint", Status: "completed", Conclusion: "success"}}}}
	if !reflect.DeepEqual(runs, want) {
		t.Fatalf("CIRuns = %#v, want only the CI workflow's run for the tagged commit %#v", runs, want)
	}

	for tag, want := range map[string]string{"v0.0.2": "4fd666747631ada5854daa820c43fb03f80851f3", "v0.9.9": ""} {
		got, err := live.PublishedCommit(ctx, tag)
		if err != nil || got != want {
			t.Fatalf("PublishedCommit(%s) = %q, %v; want %q", tag, got, err, want)
		}
	}
}

// gitRepoWith commits files (path to content) in a fresh repository and
// returns its directory and the commit.
func gitRepoWith(t *testing.T, files map[string]string) (dir, commit string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "x")
	return dir, run("rev-parse", "HEAD")
}

func TestLiveCorpusRegisteredReadsTheCommitNotTheWorkingTree(t *testing.T) {
	const versions = "package migrationcompat\n\nvar Generators = []Generator{\n\t{Dir: \"v0_2_0\", Migrations: v020.Migrations},\n\t{Dir: \"v0_3_0\", Migrations: v030.Migrations},\n}\n"
	dir, commit := gitRepoWith(t, map[string]string{
		"internal/migrationcompat/versions.go":        versions,
		"internal/migrationcompat/v0_3_0/models.json": "{}",
		"internal/migrationcompat/v0_2_0/models.json": "{}",
		"internal/migrationcompat/v0_4_0/models.json": "{}", // a directory nobody registered
	})
	live := Live{Dir: dir}

	tests := []struct {
		dir                string
		wantExists, wantIn bool
	}{
		{"v0_3_0", true, true},
		{"v0_4_0", true, false},
		{"v0_5_0", false, false},
		{"v0_3", false, false}, // a prefix of a registered name is not it
	}
	for _, tt := range tests {
		exists, registered, err := live.CorpusRegistered(context.Background(), commit, tt.dir)
		if err != nil || exists != tt.wantExists || registered != tt.wantIn {
			t.Fatalf("%s: exists=%v registered=%v err=%v, want %v %v", tt.dir, exists, registered, err, tt.wantExists, tt.wantIn)
		}
	}

	// Removing the files from the working tree changes nothing: the commit is what counts.
	if err := os.RemoveAll(filepath.Join(dir, "internal")); err != nil {
		t.Fatal(err)
	}
	if exists, registered, err := live.CorpusRegistered(context.Background(), commit, "v0_3_0"); err != nil || !exists || !registered {
		t.Fatalf("after deleting the working tree: exists=%v registered=%v err=%v, want the committed state", exists, registered, err)
	}
}

func TestLiveOnMainSaysWhetherACommitIsReachableFromOriginMain(t *testing.T) {
	dir, onMain := gitRepoWith(t, map[string]string{"a.txt": "a"})
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("update-ref", "refs/remotes/origin/main", onMain)
	git("commit", "-q", "--allow-empty", "-m", "work not on main")
	offMain := git("rev-parse", "HEAD")
	live := Live{Dir: dir}

	if got, err := live.OnMain(context.Background(), onMain); err != nil || !got {
		t.Errorf("OnMain(a commit on main) = %v, %v; want true", got, err)
	}
	if got, err := live.OnMain(context.Background(), offMain); err != nil || got {
		t.Errorf("OnMain(a commit past main) = %v, %v; want false, no error", got, err)
	}
	got, err := live.OnMain(context.Background(), "0123456789012345678901234567890123456789")
	if err == nil || got || !strings.Contains(err.Error(), "git merge-base") {
		t.Errorf("OnMain(an unknown commit) = %v, %v; want an error naming git merge-base", got, err)
	}
}
