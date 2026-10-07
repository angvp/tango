package tango_test

// This file is the tutorial snippet sync check described in ADR 0023's
// amendment: every Go block in docs/tutorial whose first line names a file
// (`// apps/posts/app.go`) must appear in that file under examples/board,
// ignoring whitespace differences. A block that shows an earlier state of
// the file says so in its label (`// apps/posts/app.go (as of part 2)`) and
// is exempt. Blocks without a file label are not checked.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestTutorialSnippetsMatchBoard(t *testing.T) {
	problems, err := tutorialSnippetProblems(filepath.Join("docs", "tutorial"), filepath.Join("examples", "board"))
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		t.Error(problem)
	}
}

func TestTutorialSnippetCheckPassesWhenBlockMatchesIgnoringWhitespace(t *testing.T) {
	tutorial, app := snippetFixture(t,
		"```go\n// apps/posts/app.go\nfunc New() int {\n    return  1\n\n}\n```\n",
		map[string]string{"apps/posts/app.go": "package posts\n\nfunc New() int {\n\treturn 1\n}\n"})

	assertSnippetProblems(t, tutorial, app)
}

func TestTutorialSnippetCheckFailsOnDivergingBlock(t *testing.T) {
	tutorial, app := snippetFixture(t,
		"```go\n// apps/posts/app.go\nfunc New() int {\n\treturn 2\n}\n```\n",
		map[string]string{"apps/posts/app.go": "package posts\n\nfunc New() int {\n\treturn 1\n}\n"})

	assertSnippetProblems(t, tutorial, app, "01-part.md:2: block labelled apps/posts/app.go does not appear in")
}

func TestTutorialSnippetCheckFailsOnLabelNamingMissingFile(t *testing.T) {
	tutorial, app := snippetFixture(t,
		"```go\n// apps/posts/missing.go\nfunc New() {}\n```\n",
		map[string]string{"apps/posts/app.go": "package posts\n"})

	assertSnippetProblems(t, tutorial, app, "01-part.md:2: block labelled apps/posts/missing.go names a file that does not exist")
}

func TestTutorialSnippetCheckRejectsPathsOutsideTheApp(t *testing.T) {
	tutorial, app := snippetFixture(t,
		"```go\n// ../secret.go\nfunc New() {}\n```\n",
		map[string]string{"apps/posts/app.go": "package posts\n"})

	assertSnippetProblems(t, tutorial, app, "01-part.md:2: label ../secret.go is not a path inside")
}

func TestTutorialSnippetCheckExemptsEarlierStateBlocks(t *testing.T) {
	tutorial, app := snippetFixture(t,
		"```go\n// apps/posts/app.go (as of part 2)\nfunc New() int {\n\treturn 2\n}\n```\n",
		map[string]string{"apps/posts/app.go": "package posts\n\nfunc New() int {\n\treturn 1\n}\n"})

	assertSnippetProblems(t, tutorial, app)
}

func TestTutorialSnippetCheckFailsOnMalformedMarker(t *testing.T) {
	tutorial, app := snippetFixture(t,
		"```go\n// apps/posts/app.go (as of part two)\nfunc New() int {\n\treturn 2\n}\n```\n",
		map[string]string{"apps/posts/app.go": "package posts\n"})

	assertSnippetProblems(t, tutorial, app, "01-part.md:2: label \"// apps/posts/app.go (as of part two)\" has text after the path that is not \"(as of part N)\"")
}

func TestTutorialSnippetCheckIgnoresUnlabelledAndNonGoBlocks(t *testing.T) {
	tutorial, app := snippetFixture(t,
		"```go\nfunc nowhere() {}\n```\n\n```sh\n// apps/posts/app.go\ntango migrate\n```\n",
		map[string]string{"apps/posts/app.go": "package posts\n"})

	assertSnippetProblems(t, tutorial, app)
}

func TestTutorialSnippetCheckMatchesWholeLinesOnly(t *testing.T) {
	tutorial, app := snippetFixture(t,
		"```go\n// apps/posts/app.go\nreturn 1\n```\n",
		map[string]string{"apps/posts/app.go": "package posts\n\nfunc New() int {\n\treturn 10\n}\n"})

	assertSnippetProblems(t, tutorial, app, "01-part.md:2: block labelled apps/posts/app.go does not appear in")
}

// snippetFixture writes one tutorial part and the given app files into a
// temporary directory and returns the tutorial and app directories.
func snippetFixture(t *testing.T, part string, files map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	tutorial := filepath.Join(root, "tutorial")
	app := filepath.Join(root, "board")
	writeFixtureFile(t, filepath.Join(tutorial, "01-part.md"), part)
	for name, content := range files {
		writeFixtureFile(t, filepath.Join(app, filepath.FromSlash(name)), content)
	}
	return tutorial, app
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertSnippetProblems checks that the check reports exactly the wanted
// problems, each matched by prefix, in order.
func assertSnippetProblems(t *testing.T, tutorial, app string, wantPrefixes ...string) {
	t.Helper()
	problems, err := tutorialSnippetProblems(tutorial, app)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != len(wantPrefixes) {
		t.Fatalf("got %d problems %q, want %d %q", len(problems), problems, len(wantPrefixes), wantPrefixes)
	}
	for i, want := range wantPrefixes {
		if !strings.HasPrefix(problems[i], want) {
			t.Errorf("problem %d = %q, want prefix %q", i, problems[i], want)
		}
	}
}

// earlierStateMarker is the only text a label may carry after its path.
var earlierStateMarker = regexp.MustCompile(`^\(as of part [1-9][0-9]*\)$`)

// tutorialSnippetProblems checks every Markdown file in tutorialDir against
// the app in appDir and returns one message per failing block.
func tutorialSnippetProblems(tutorialDir, appDir string) ([]string, error) {
	parts, err := filepath.Glob(filepath.Join(tutorialDir, "*.md"))
	if err != nil {
		return nil, fmt.Errorf("list tutorial parts: %w", err)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("no tutorial parts in %s", tutorialDir)
	}
	var problems []string
	for _, part := range parts {
		content, err := os.ReadFile(part)
		if err != nil {
			return nil, fmt.Errorf("read tutorial part: %w", err)
		}
		for _, block := range goBlocks(string(content)) {
			if problem := checkSnippet(block, appDir); problem != "" {
				problems = append(problems, fmt.Sprintf("%s:%d: %s", filepath.Base(part), block.line, problem))
			}
		}
	}
	return problems, nil
}

// goBlock is one fenced Go code block; line is its first line's number.
type goBlock struct {
	line  int
	lines []string
}

func goBlocks(markdown string) []goBlock {
	var blocks []goBlock
	var current *goBlock
	for i, line := range strings.Split(markdown, "\n") {
		fence := strings.TrimSpace(line)
		switch {
		case current == nil && fence == "```go":
			current = &goBlock{line: i + 2}
		case current != nil && strings.HasPrefix(fence, "```"):
			blocks = append(blocks, *current)
			current = nil
		case current != nil:
			current.lines = append(current.lines, line)
		}
	}
	return blocks
}

// checkSnippet returns why block fails the check, or "" if it passes or is
// not checked.
func checkSnippet(block goBlock, appDir string) string {
	if len(block.lines) == 0 {
		return ""
	}
	label := strings.TrimSpace(block.lines[0])
	fields := strings.Fields(strings.TrimPrefix(label, "//"))
	if !strings.HasPrefix(label, "//") || len(fields) == 0 || !strings.HasSuffix(fields[0], ".go") {
		return ""
	}
	path := fields[0]
	rest := strings.Join(fields[1:], " ")
	switch {
	case rest != "" && !earlierStateMarker.MatchString(rest):
		return fmt.Sprintf("label %q has text after the path that is not \"(as of part N)\"", label)
	case !filepath.IsLocal(filepath.FromSlash(path)):
		return fmt.Sprintf("label %s is not a path inside %s", path, appDir)
	}
	target := filepath.Join(appDir, filepath.FromSlash(path))
	source, err := os.ReadFile(target)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Sprintf("block labelled %s names a file that does not exist: %s", path, target)
	}
	if err != nil {
		return fmt.Sprintf("block labelled %s: %v", path, err)
	}
	if rest != "" {
		return "" // an earlier state of a file that exists
	}
	if !strings.Contains(normalizeLines(string(source)), normalizeLines(strings.Join(block.lines[1:], "\n"))) {
		return fmt.Sprintf("block labelled %s does not appear in %s (ignoring whitespace); fix the block or the file, or mark the label \"(as of part N)\" if it shows an earlier state", path, target)
	}
	return ""
}

// normalizeLines collapses each line's whitespace to single spaces and drops
// blank lines. The result starts and ends with a newline so that matching
// one normalized text inside another only ever matches whole lines.
func normalizeLines(text string) string {
	var b strings.Builder
	b.WriteString("\n")
	for _, line := range strings.Split(text, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			b.WriteString(strings.Join(fields, " "))
			b.WriteString("\n")
		}
	}
	return b.String()
}
