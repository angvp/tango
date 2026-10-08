package releasecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestLiveReadsGitHubAndTheModuleProxy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/angvp/tango/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("head_sha") != commit || r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Write([]byte(`{"workflow_runs":[
			{"id":1,"path":".github/workflows/ci.yml","head_sha":"` + commit + `","status":"completed","conclusion":"success"},
			{"id":2,"path":".github/workflows/release.yml","head_sha":"` + commit + `","status":"completed","conclusion":"success"}]}`))
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

	runs, err := live.CIRuns(ctx, commit)
	if err != nil {
		t.Fatalf("CIRuns: %v", err)
	}
	want := []CIRun{{Status: "completed", Conclusion: "success", Jobs: []CIJob{{Name: "lint", Status: "completed", Conclusion: "success"}}}}
	if !reflect.DeepEqual(runs, want) {
		t.Fatalf("CIRuns = %#v, want only the CI workflow's run %#v", runs, want)
	}

	for tag, want := range map[string]string{"v0.0.2": "4fd666747631ada5854daa820c43fb03f80851f3", "v0.9.9": ""} {
		got, err := live.PublishedCommit(ctx, tag)
		if err != nil || got != want {
			t.Fatalf("PublishedCommit(%s) = %q, %v; want %q", tag, got, err, want)
		}
	}
}
