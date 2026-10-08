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
	tagged, err := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", tag+"^{commit}").Output()
	tagExists := err == nil
	if commit == "" {
		if !tagExists {
			return fmt.Errorf("tag %s doesn't exist: pass -commit to check a release before tagging it", tag)
		}
		commit = strings.TrimSpace(string(tagged))
	}
	changelog, err := os.ReadFile(changelogPath)
	if err != nil {
		return fmt.Errorf("read the changelog: %w", err)
	}
	var report []byte
	if goreleasePath != "" {
		if report, err = os.ReadFile(goreleasePath); err != nil {
			return fmt.Errorf("read the gorelease report: %w", err)
		}
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	var sources releasecheck.Sources = releasecheck.Live{Dir: ".", Repo: repo, Module: "github.com/angvp/tango", Token: token}
	if !tagExists {
		sources = notYetTagged{sources}
	}
	release := releasecheck.Release{Tag: tag, Commit: commit, Changelog: changelog, Gorelease: string(report)}
	body, err := releasecheck.Check(ctx, release, sources)
	if err != nil {
		return err
	}
	fmt.Printf("%s at %s may be released.\n", tag, commit)
	if notesPath != "" {
		if err := os.WriteFile(notesPath, []byte(body+"\n"), 0o644); err != nil {
			return fmt.Errorf("write the release notes: %w", err)
		}
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// notYetTagged checks a release before its tag exists, so nothing can be
// published under it yet. It never asks the module proxy: the proxy caches
// "unknown revision" for a while, which would delay the real release.
type notYetTagged struct{ releasecheck.Sources }

func (notYetTagged) PublishedCommit(context.Context, string) (string, error) { return "", nil }
