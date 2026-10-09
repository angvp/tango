// Package releasecheck decides whether a pushed tag may become a tanGO
// release. The release workflow runs it (see cmd/releasecheck) and creates
// the GitHub release only when it passes; RELEASING.md describes the whole
// process.
package releasecheck

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// RequiredJobs are the CI jobs that must all succeed on the exact commit a
// release tags.
var RequiredJobs = []string{"lint", "test (sqlite)", "test (postgres)"}

// Release is a pushed tag and what the workflow gathered about it.
type Release struct {
	Tag       string // the pushed tag, e.g. v0.1.1
	Commit    string // the full SHA the tag points to
	Changelog []byte // CHANGELOG.md at that commit
	// Gorelease is gorelease's report comparing the tagged commit with the
	// previous release, or "" if it wasn't run.
	Gorelease string
}

// Sources looks up what the tag alone doesn't say.
type Sources interface {
	// OnMain reports whether commit is reachable from main.
	OnMain(ctx context.Context, commit string) (bool, error)
	// CIRuns returns every CI workflow run for exactly commit.
	CIRuns(ctx context.Context, commit string) ([]CIRun, error)
	// PublishedCommit returns the commit the Go module proxy already serves
	// for tag, or "" if it doesn't know the tag.
	PublishedCommit(ctx context.Context, tag string) (string, error)
	// CorpusRegistered reports, as of commit, whether the migration-compat
	// fixture directory dir (such as v0_3_0) exists, and whether it is
	// listed in the Generators the Generated-file contract tests read.
	CorpusRegistered(ctx context.Context, commit, dir string) (exists, registered bool, err error)
}

// CIRun is one CI workflow run and its jobs.
type CIRun struct {
	Status     string // queued, in_progress, completed, ...
	Conclusion string // success, failure, ... once completed
	Jobs       []CIJob
}

// CIJob is one job of a CIRun.
type CIJob struct {
	Name       string
	Status     string
	Conclusion string
}

var releaseTag = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)

// Check returns the release notes for r — its CHANGELOG.md section — or an
// error listing every reason r may not be released.
func Check(ctx context.Context, r Release, s Sources) (string, error) {
	match := releaseTag.FindStringSubmatch(r.Tag)
	if match == nil {
		return "", fmt.Errorf("tag %q is not a release tag: releases are tagged vMAJOR.MINOR.PATCH", r.Tag)
	}
	patch, _ := strconv.Atoi(match[3])

	var problems []error
	if onMain, err := s.OnMain(ctx, r.Commit); err != nil {
		problems = append(problems, fmt.Errorf("check commit %s is on main: %w", r.Commit, err))
	} else if !onMain {
		problems = append(problems, fmt.Errorf("commit %s is not reachable from main: release only commits on main", r.Commit))
	}
	notes, err := changelogSection(r.Changelog, strings.TrimPrefix(r.Tag, "v"))
	if err != nil {
		problems = append(problems, err)
	}
	if err := checkCorpus(ctx, r, match, s); err != nil {
		problems = append(problems, err)
	}
	if err := checkCI(ctx, r.Commit, s); err != nil {
		problems = append(problems, err)
	}
	if patch > 0 {
		if err := checkPatchCompatible(r.Gorelease); err != nil {
			problems = append(problems, err)
		}
	}
	if len(problems) > 0 {
		return "", errors.Join(problems...)
	}
	// Last, and only for an otherwise ready release: asking the proxy about
	// a tag makes it fetch and keep that tag's commit for good, and a
	// refused tag must stay fixable.
	if err := checkPublished(ctx, r, s); err != nil {
		return "", err
	}
	return notes, nil
}

// checkPublished refuses a tag the Go module proxy already serves from a
// different commit: the proxy and checksum database never change, so users
// would keep getting the old commit whatever the tag now says.
func checkPublished(ctx context.Context, r Release, s Sources) error {
	published, err := s.PublishedCommit(ctx, r.Tag)
	if err != nil {
		return fmt.Errorf("look up %s on the Go module proxy: %w", r.Tag, err)
	}
	if published != "" && published != r.Commit {
		return fmt.Errorf("%s is already published for commit %s, not %s: never move a release tag; release a new version instead", r.Tag, published, r.Commit)
	}
	return nil
}

// checkCI requires one CI run for exactly commit in which every required job
// succeeded.
func checkCI(ctx context.Context, commit string, s Sources) error {
	runs, err := s.CIRuns(ctx, commit)
	if err != nil {
		return fmt.Errorf("look up CI runs for %s: %w", commit, err)
	}
	running := false
	var missing []string
	for _, run := range runs {
		failed := failedRequiredJobs(run)
		if len(failed) == 0 {
			return nil
		}
		// A run still going can yet pass, unless a required job already failed.
		if run.Status != "completed" && !anyRequiredJobFailed(run) {
			running = true
		}
		missing = append(missing, failed...)
	}
	if running {
		return fmt.Errorf("CI is still running for %s: re-run this workflow once it finishes", commit)
	}
	if len(runs) == 0 {
		return fmt.Errorf("no successful CI run for %s: CI has not run on this exact commit", commit)
	}
	return fmt.Errorf("no successful CI run for %s: required jobs not successful: %s", commit, strings.Join(unique(missing), ", "))
}

// failedRequiredJobs lists the required jobs run didn't complete successfully.
func failedRequiredJobs(run CIRun) []string {
	var failed []string
	for _, name := range RequiredJobs {
		ok := false
		for _, job := range run.Jobs {
			if job.Name == name && job.Status == "completed" && job.Conclusion == "success" {
				ok = true
			}
		}
		if !ok {
			failed = append(failed, name)
		}
	}
	return failed
}

// anyRequiredJobFailed reports whether a required job of run finished
// without succeeding.
func anyRequiredJobFailed(run CIRun) bool {
	for _, job := range run.Jobs {
		if slices.Contains(RequiredJobs, job.Name) && job.Status == "completed" && job.Conclusion != "success" {
			return true
		}
	}
	return false
}

// checkPatchCompatible refuses a patch release whose gorelease report lists
// any incompatible change, in any package.
func checkPatchCompatible(report string) error {
	if strings.TrimSpace(report) == "" {
		return errors.New("a patch release needs gorelease's report against the previous release, and there is none")
	}
	var incompatible []string
	pkg, inIncompatible := "", false
	for _, line := range strings.Split(report, "\n") {
		switch {
		case strings.HasPrefix(line, "# "):
			pkg, inIncompatible = strings.TrimPrefix(line, "# "), false
		case strings.HasPrefix(line, "## "):
			inIncompatible = line == "## incompatible changes"
		case inIncompatible && strings.TrimSpace(line) != "":
			incompatible = append(incompatible, pkg+": "+line)
		}
	}
	if len(incompatible) > 0 {
		return fmt.Errorf("a patch release may not make incompatible changes, and gorelease reports:\n%s", strings.Join(incompatible, "\n"))
	}
	return nil
}

var sectionHeading = regexp.MustCompile(`^## \[([^\]]+)\](?: - ([0-9]{4}-[0-9]{2}-[0-9]{2}))?\s*$`)

// changelogSection returns the body of version's section of a Keep a
// Changelog file, which must be dated.
func changelogSection(changelog []byte, version string) (string, error) {
	lines := strings.Split(string(changelog), "\n")
	for i, line := range lines {
		match := sectionHeading.FindStringSubmatch(line)
		if match == nil || match[1] != version {
			continue
		}
		if match[2] == "" {
			return "", fmt.Errorf("CHANGELOG.md's section for %s has no date: write it as \"## [%s] - YYYY-MM-DD\"", version, version)
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			nextSection := strings.HasPrefix(lines[j], "## ")
			linkDefinition := strings.HasPrefix(lines[j], "[") && strings.Contains(lines[j], "]: ")
			if nextSection || linkDefinition {
				end = j
				break
			}
		}
		return strings.TrimSpace(strings.Join(lines[i+1:end], "\n")), nil
	}
	return "", fmt.Errorf("CHANGELOG.md has no section for %s: add \"## [%s] - YYYY-MM-DD\"", version, version)
}

func unique(values []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// checkCorpus refuses a release whose migration files are not in the
// Generated-file contract tests. Every release's own `tango makemigrations`
// output is held to the promise on later releases, and forgetting to add it
// is silent, so the tag is where it is caught.
func checkCorpus(ctx context.Context, r Release, version []string, s Sources) error {
	dir := fmt.Sprintf("v%s_%s_%s", version[1], version[2], version[3])
	exists, registered, err := s.CorpusRegistered(ctx, r.Commit, dir)
	switch {
	case err != nil:
		return fmt.Errorf("check the migration-compat corpus %s: %w", dir, err)
	case !exists:
		return fmt.Errorf("no migration-compat corpus %s at commit %s: run internal/migrationcompat/generate.sh %s local, add it to Generators in internal/migrationcompat/versions.go and commit it (see RELEASING.md)", dir, r.Commit, r.Tag)
	case !registered:
		return fmt.Errorf("migration-compat corpus %s is not listed in Generators in internal/migrationcompat/versions.go at commit %s, so the Generated-file contract tests never read it", dir, r.Commit)
	}
	return nil
}
