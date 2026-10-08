// Command releasecheck decides whether a pushed tag may become a tanGO
// release, and writes its release notes. The release workflow runs it; a
// maintainer can run it locally too (see RELEASING.md):
//
//	go run ./internal/releasecheck/cmd/releasecheck -tag v0.1.0 -gorelease report.txt
//
// It exits 1, listing every problem, when the release must not happen.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/angvp/tango/internal/releasecheck"
)

func main() {
	tag := flag.String("tag", "", "the release tag, e.g. v0.1.0")
	commit := flag.String("commit", "", "the commit the tag points to (default: resolved from the tag)")
	changelog := flag.String("changelog", "CHANGELOG.md", "the changelog at that commit")
	gorelease := flag.String("gorelease", "", "a file holding gorelease's report against the previous release")
	notes := flag.String("notes", "", "write the release notes to this file")
	repo := flag.String("repo", envOr("GITHUB_REPOSITORY", "angvp/tango"), "the GitHub repository")
	flag.Parse()
	if err := run(*tag, *commit, *changelog, *gorelease, *notes, *repo); err != nil {
		fmt.Fprintln(os.Stderr, "releasecheck:", err)
		os.Exit(1)
	}
}

func run(tag, commit, changelogPath, goreleasePath, notesPath, repo string) error {
	ctx := context.Background()
	if tag == "" {
		return fmt.Errorf("-tag is required")
	}
	if commit == "" {
		out, err := exec.CommandContext(ctx, "git", "rev-parse", tag+"^{commit}").Output()
		if err != nil {
			return fmt.Errorf("resolve %s: %w", tag, err)
		}
		commit = strings.TrimSpace(string(out))
	}
	changelog, err := os.ReadFile(changelogPath)
	if err != nil {
		return err
	}
	var report []byte
	if goreleasePath != "" {
		if report, err = os.ReadFile(goreleasePath); err != nil {
			return err
		}
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	sources := releasecheck.Live{Dir: ".", Repo: repo, Module: "github.com/angvp/tango", Token: token}
	release := releasecheck.Release{Tag: tag, Commit: commit, Changelog: changelog, Gorelease: string(report)}
	body, err := releasecheck.Check(ctx, release, sources)
	if err != nil {
		return err
	}
	fmt.Printf("%s at %s may be released.\n", tag, commit)
	if notesPath != "" {
		return os.WriteFile(notesPath, []byte(body+"\n"), 0o644)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
