package tango_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The agent context pack has one recipe index, docs/agents/README.md, and one
// import guardrail, the public-package line in AGENTS.md. These checks keep
// both honest without judging any prose: they compare names only.

var recipeFileName = regexp.MustCompile("`([a-z0-9-]+\\.md)`")

// notImportable are public packages an application should not import, so the
// guardrail line need not name them: conformance suites for adapter authors.
var notImportable = []string{"cachetest", "storagetest"}

// indexProblems compares the README's recipe links with the recipe files
// (base names): a recipe the README never links, and a link to no recipe.
func indexProblems(readme string, recipes []string) []string {
	linked := map[string]bool{}
	for _, m := range recipeFileName.FindAllStringSubmatch(readme, -1) {
		linked[m[1]] = true
	}
	var problems []string
	for _, recipe := range recipes {
		if !linked[recipe] {
			problems = append(problems, "README does not link "+recipe)
		}
	}
	for name := range linked {
		if !slices.Contains(recipes, name) && !slices.Contains(notRecipes, name) {
			problems = append(problems, "README links "+name+", which is not a recipe file")
		}
	}
	slices.Sort(problems)
	return problems
}

// packageProblems lists the public packages the AGENTS.md text never names in
// backticks and that are not in the not-importable list.
func packageProblems(agents string, packages []string) []string {
	var problems []string
	for _, pkg := range packages {
		if slices.Contains(notImportable, pkg) || strings.Contains(agents, "`"+pkg+"`") {
			continue
		}
		problems = append(problems, "AGENTS.md does not name package "+pkg)
	}
	return problems
}

func TestIndexProblemsFindsOrphansAndDanglingLinks(t *testing.T) {
	readme := "- `a.md` and `b.md`, `checklist.md`, `gone.md`."
	got := indexProblems(readme, []string{"a.md", "b.md", "orphan.md"})
	want := []string{"README does not link orphan.md", "README links gone.md, which is not a recipe file"}
	if !slices.Equal(got, want) {
		t.Fatalf("problems = %q, want %q", got, want)
	}
	if got := indexProblems(readme, []string{"a.md", "b.md", "gone.md"}); len(got) != 0 {
		t.Fatalf("clean index reported %q", got)
	}
}

func TestPackageProblemsNamesTheUndocumentedOnes(t *testing.T) {
	agents := "Use `tango`, `model` and `cache/local`."
	got := packageProblems(agents, []string{"tango", "model", "cache/local", "storage", "cachetest"})
	if want := []string{"AGENTS.md does not name package storage"}; !slices.Equal(got, want) {
		t.Fatalf("problems = %q, want %q", got, want)
	}
}

func recipeFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("docs/agents/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("no agent docs found: %v", err)
	}
	var recipes []string
	for _, file := range files {
		if base := filepath.Base(file); !slices.Contains(notRecipes, base) {
			recipes = append(recipes, base)
		}
	}
	return recipes
}

func TestAgentReadmeIndexesEveryRecipeAndOnlyRecipes(t *testing.T) {
	readme, err := os.ReadFile("docs/agents/README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range indexProblems(string(readme), recipeFiles(t)) {
		t.Error(problem)
	}
}

func TestAgentsMdPointsAtTheRecipeIndex(t *testing.T) {
	agents, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agents), "docs/agents/README.md") {
		t.Error("AGENTS.md does not point at docs/agents/README.md")
	}
}

// publicPackages lists the import paths of every package an application could
// import: directories with non-test Go files outside internal, examples, cmd,
// scripts, docs and testdata. The root package is "tango".
func publicPackages(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != "." && (strings.HasPrefix(name, ".") || slices.Contains([]string{"internal", "examples", "cmd", "scripts", "docs", "testdata"}, name)) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			seen[filepath.ToSlash(filepath.Dir(path))] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var packages []string
	for dir := range seen {
		if dir == "." {
			dir = "tango"
		}
		packages = append(packages, dir)
	}
	slices.Sort(packages)
	return packages
}

func TestAgentsMdNamesEveryPublicPackage(t *testing.T) {
	agents, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range packageProblems(string(agents), publicPackages(t)) {
		t.Error(problem)
	}
}
