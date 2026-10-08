package tango_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// notRecipes are the agent docs that aren't task recipes: the index, the
// shared checklist and the prompt library.
var notRecipes = []string{"README.md", "checklist.md", "prompts.md"}

var (
	backticked   = regexp.MustCompile("`([^`\\s]+)`")
	markdownLink = regexp.MustCompile(`\]\(([^)\s#]+)`)
	guideRef     = regexp.MustCompile(`(?:docs/|\.\./)guides/[a-z0-9-]+\.md`)
	fileName     = regexp.MustCompile(`^[\w.-]+\.[a-z]+$`)
)

// looksLikePath reports whether a backticked token names a file or
// directory rather than a Go identifier such as `RequireVerified`.
func looksLikePath(token string) bool {
	return strings.Contains(token, "/") || fileName.MatchString(token) || token == "Dockerfile"
}

// recipeProblems lists what's wrong with one agent recipe's references, as
// paths relative to the repository root: a canonical file, a named example
// or docs page, or a linked file that doesn't exist, and a recipe that
// links no human guide. It doesn't look at Go identifiers.
func recipeProblems(text string, exists func(path string) bool) []string {
	var problems []string
	check := func(path string) {
		path = strings.TrimSuffix(strings.TrimRight(path, ".,;:"), "/")
		if !exists(path) {
			problems = append(problems, "missing "+path)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		canonical := strings.HasPrefix(line, "Canonical")
		for _, m := range backticked.FindAllStringSubmatch(line, -1) {
			token := m[1]
			if (canonical && looksLikePath(token)) || strings.HasPrefix(token, "examples/") || strings.HasPrefix(token, "docs/") {
				check(token)
			}
		}
		for _, m := range markdownLink.FindAllStringSubmatch(line, -1) {
			if !strings.Contains(m[1], "://") {
				check(filepath.ToSlash(filepath.Join("docs/agents", m[1])))
			}
		}
	}
	if !guideRef.MatchString(text) {
		problems = append(problems, "links no human guide (docs/guides/…)")
	}
	return problems
}

func TestRecipeProblemsFindsBrokenReferences(t *testing.T) {
	exists := func(path string) bool {
		return slices.Contains([]string{"middleware.go", "examples/board", "docs/guides/routing-and-reverse-lookup.md", "docs/agents/checklist.md"}, path)
	}
	tests := []struct {
		name, recipe string
		want         []string
	}{
		{"clean", "Canonical files: `middleware.go`.\n\nSee `docs/guides/routing-and-reverse-lookup.md`, `examples/board/` and [the checklist](checklist.md). Put ports in `ports/`.", nil},
		{"missing canonical file", "Canonical files: `gone.go`.\n\nSee `docs/guides/routing-and-reverse-lookup.md`.", []string{"missing gone.go"}},
		{"missing example", "Proof: `examples/nowhere`. See `docs/guides/routing-and-reverse-lookup.md`.", []string{"missing examples/nowhere"}},
		{"missing linked page", "See [x](../guides/gone.md) and `docs/guides/routing-and-reverse-lookup.md`.", []string{"missing docs/guides/gone.md"}},
		{"no guide", "Canonical files: `middleware.go`.", []string{"links no human guide (docs/guides/…)"}},
		{"identifiers aren't paths", "Canonical files: `middleware.go` (`RequestID`, `slog`, `mailtest.Sender`). See `docs/guides/routing-and-reverse-lookup.md`.", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recipeProblems(tt.recipe, exists); !slices.Equal(got, tt.want) {
				t.Fatalf("problems = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAgentRecipesReferenceRealFilesAndAGuide keeps docs/agents from
// drifting: every recipe's paths exist and every recipe links a human
// guide.
func TestAgentRecipesReferenceRealFilesAndAGuide(t *testing.T) {
	files, err := filepath.Glob("docs/agents/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("no recipes found: %v", err)
	}
	exists := func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}
	for _, file := range files {
		if slices.Contains(notRecipes, filepath.Base(file)) {
			continue
		}
		text, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, problem := range recipeProblems(string(text), exists) {
			t.Errorf("%s: %s", file, problem)
		}
	}
}
