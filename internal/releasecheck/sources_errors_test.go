package releasecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The module proxy answers 404 or 410 for a version it does not serve, and
// that is an answer: no commit, no error. Any other failure is an error.
func TestPublishedCommitTreatsOnlyMissingVersionsAsUnpublished(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr string
	}{
		{name: "not found is unpublished", status: http.StatusNotFound, want: ""},
		{name: "gone is unpublished", status: http.StatusGone, want: ""},
		{name: "server failure is an error", status: http.StatusInternalServerError, wantErr: "500"},
		{name: "malformed info is an error", status: http.StatusOK, body: `{"Origin":`, wantErr: "unexpected EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()
			live := Live{Module: "github.com/angvp/tango", Proxy: server.URL}

			got, err := live.PublishedCommit(context.Background(), "v0.0.2")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("PublishedCommit error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("PublishedCommit = %q, %v; want %q, nil", got, err, tt.want)
			}
		})
	}
}

// A failed GitHub request is reported, whichever of the two calls fails:
// the runs list, or the jobs of a run it found.
func TestCIRunsReportsAFailedGitHubRequest(t *testing.T) {
	const runsPath = "GET /repos/angvp/tango/actions/runs"
	const jobsPath = "GET /repos/angvp/tango/actions/runs/1/jobs"
	run := `{"workflow_runs":[{"id":1,"path":".github/workflows/ci.yml","head_sha":"` + taggedCommit + `","status":"completed","conclusion":"success"}]}`

	tests := []struct {
		name     string
		runsCode int
		runsBody string
		jobsCode int
	}{
		{name: "runs list fails", runsCode: http.StatusInternalServerError, jobsCode: http.StatusOK},
		{name: "jobs of a run fail", runsCode: http.StatusOK, runsBody: run, jobsCode: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc(runsPath, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.runsCode)
				w.Write([]byte(tt.runsBody))
			})
			mux.HandleFunc(jobsPath, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.jobsCode)
				w.Write([]byte(`{"jobs":[]}`))
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			live := Live{Repo: "angvp/tango", API: server.URL}

			runs, err := live.CIRuns(context.Background(), taggedCommit)
			if err == nil {
				t.Fatalf("CIRuns = %#v, nil; want the GitHub failure reported", runs)
			}
			if runs != nil {
				t.Fatalf("CIRuns returned runs %#v alongside the error", runs)
			}
		})
	}
}
