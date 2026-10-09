package releasecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
)

const (
	// ciWorkflow is the workflow whose runs prove a commit passed CI.
	ciWorkflow = ".github/workflows/ci.yml"
	// defaultAPI and defaultProxy are where Live looks unless told otherwise.
	defaultAPI   = "https://api.github.com"
	defaultProxy = "https://proxy.golang.org"
)

// Live looks things up in the git checkout, the GitHub Actions API and the
// Go module proxy.
type Live struct {
	Dir    string       // the git checkout, with main fetched as origin/main
	Repo   string       // owner/name on GitHub
	Module string       // the module path, e.g. github.com/angvp/tango
	Token  string       // a GitHub token that can read Actions runs
	Client *http.Client // nil means http.DefaultClient
	API    string       // "" means https://api.github.com
	Proxy  string       // "" means https://proxy.golang.org
}

// OnMain asks git whether commit is an ancestor of origin/main.
func (l Live) OnMain(ctx context.Context, commit string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", commit, "origin/main")
	cmd.Dir = l.Dir
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return false, nil
	default:
		return false, fmt.Errorf("git merge-base: %w: %s", err, strings.TrimSpace(string(out)))
	}
}

// CorpusRegistered reads commit, not the working tree: the fixture directory
// must be in the tree being tagged, and versions.go at that commit must list
// it as a generator.
func (l Live) CorpusRegistered(ctx context.Context, commit, dir string) (exists, registered bool, err error) {
	const base = "internal/migrationcompat/"
	git := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = l.Dir
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return out, nil
	}
	tree, err := git("ls-tree", "-d", "--name-only", commit, base+dir)
	if err != nil {
		return false, false, err
	}
	if strings.TrimSpace(string(tree)) == "" {
		return false, false, nil
	}
	versions, err := git("show", commit+":"+base+"versions.go")
	if err != nil {
		return true, false, err
	}
	return true, strings.Contains(string(versions), `Dir: "`+dir+`"`), nil
}

// CIRuns returns the CI workflow's runs for commit, each with the jobs of
// its latest attempt.
func (l Live) CIRuns(ctx context.Context, commit string) ([]CIRun, error) {
	var runs struct {
		WorkflowRuns []struct {
			ID         int64  `json:"id"`
			Path       string `json:"path"`
			HeadSHA    string `json:"head_sha"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"workflow_runs"`
	}
	if err := l.getJSON(ctx, l.api()+"/repos/"+l.Repo+"/actions/runs?per_page=100&head_sha="+commit, &runs); err != nil {
		return nil, err
	}
	var out []CIRun
	for _, run := range runs.WorkflowRuns {
		if run.Path != ciWorkflow || run.HeadSHA != commit {
			continue
		}
		var jobs struct {
			Jobs []CIJob `json:"jobs"`
		}
		if err := l.getJSON(ctx, fmt.Sprintf("%s/repos/%s/actions/runs/%d/jobs?per_page=100", l.api(), l.Repo, run.ID), &jobs); err != nil {
			return nil, err
		}
		out = append(out, CIRun{Status: run.Status, Conclusion: run.Conclusion, Jobs: jobs.Jobs})
	}
	return out, nil
}

// PublishedCommit asks the Go module proxy which commit it serves for tag.
func (l Live) PublishedCommit(ctx context.Context, tag string) (string, error) {
	proxy := l.Proxy
	if proxy == "" {
		proxy = defaultProxy
	}
	url := proxy + "/" + strings.ToLower(l.Module) + "/@v/" + tag + ".info"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build module proxy request: %w", err)
	}
	resp, err := l.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("query module proxy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", req.URL, resp.Status)
	}
	var info struct {
		Origin struct {
			Hash string `json:"Hash"`
		} `json:"Origin"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("%s: %w", req.URL, err)
	}
	return info.Origin.Hash, nil
}

// CIJob decodes from the GitHub API's job objects.
func (j *CIJob) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*j = CIJob(raw)
	return nil
}

func (l Live) getJSON(ctx context.Context, url string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if l.Token != "" {
		req.Header.Set("Authorization", "Bearer "+l.Token)
	}
	resp, err := l.client().Do(req)
	if err != nil {
		return fmt.Errorf("query GitHub API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}
	return nil
}

func (l Live) api() string {
	if l.API != "" {
		return l.API
	}
	return defaultAPI
}

func (l Live) client() *http.Client {
	if l.Client != nil {
		return l.Client
	}
	return http.DefaultClient
}
