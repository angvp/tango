package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango/internal/shellcore"
)

// The shell guide, the tutorial and the agent recipe quote sessions. These
// tests keep every quoted line and its output equal to the tested sessions in
// testdata, so the documentation cannot describe a shell that does not exist.

var textBlock = regexp.MustCompile("(?s)```text\n(.*?)```")

// quotedPairs reads the typed lines ("tango> …") of every ```text block of a
// document and the output lines after each.
func quotedPairs(t *testing.T, path string) []transcriptPair {
	t.Helper()
	doc, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var pairs []transcriptPair
	for _, block := range textBlock.FindAllStringSubmatch(string(doc), -1) {
		start := len(pairs)
		for _, line := range strings.Split(strings.TrimRight(block[1], "\n"), "\n") {
			if input, ok := strings.CutPrefix(line, "tango> "); ok {
				pairs = append(pairs, transcriptPair{input: input})
			} else if len(pairs) > start {
				last := &pairs[len(pairs)-1]
				if last.output != "" {
					last.output += "\n"
				}
				last.output += line
			}
		}
	}
	return pairs
}

func normalizeSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func TestShellDocsQuoteTheTestedSession(t *testing.T) {
	tested := append(loadTranscript(t, "shell_session.txt"), loadTranscript(t, "shell_helpers_session.txt")...)
	known := map[transcriptPair]bool{}
	for _, p := range tested {
		known[p] = true
	}
	docs := []string{"../../docs/guides/shell.md", "../../docs/tutorial/03-admin.md", "../../docs/agents/shell.md"}
	for _, path := range docs {
		pairs := quotedPairs(t, path)
		if len(pairs) == 0 {
			t.Errorf("%s quotes no shell session", path)
		}
		for _, p := range pairs {
			if !known[p] {
				t.Errorf("%s shows\n  tango> %s\n  %s\nwhich no tested session in testdata contains. Change the doc and the session together.", path, p.input, strings.ReplaceAll(p.output, "\n", "\n  "))
			}
		}
	}

	// The guide and the tutorial walk the whole first session, in order.
	for _, path := range docs[:2] {
		var inOrder []transcriptPair
		for _, p := range quotedPairs(t, path) {
			if known[p] {
				inOrder = append(inOrder, p)
			}
		}
		walk := loadTranscript(t, "shell_session.txt")
		if len(inOrder) < len(walk) {
			t.Errorf("%s quotes %d of the %d steps of the tested session", path, len(inOrder), len(walk))
			continue
		}
		for i, want := range walk {
			if inOrder[i] != want {
				t.Errorf("%s step %d is %q, want %q (the tested session's order)", path, i+1, inOrder[i].input, want.input)
				break
			}
		}
	}
}

func TestShellGuideCarriesTheTestedSnippetsAndTables(t *testing.T) {
	guideBytes, err := os.ReadFile("../../docs/guides/shell.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := string(guideBytes)

	snippet, err := os.ReadFile("testdata/shell_helpers_snippet.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(normalizeSpace(guide), normalizeSpace(string(snippet))) {
		t.Errorf("the guide does not show the Helpers snippet the end-to-end test registers (testdata/shell_helpers_snippet.txt)")
	}

	// The limits table is the interpreter's own list, row for row.
	for _, l := range shellcore.Limitations {
		row := "| " + strings.ToUpper(l.Feature[:1]) + l.Feature[1:] + " | " + l.Workaround + " |"
		if !strings.Contains(guide, row) {
			t.Errorf("the guide's \"what the interpreter cannot run\" table lacks the row %q", row)
		}
	}
	if !strings.Contains(guide, "pinned at v0.16.1") {
		t.Errorf("the guide does not name the pinned interpreter version")
	}

	// The upgrade section shows the change to main.go that the upgrade test applies.
	diff, err := os.ReadFile("testdata/legacy_scaffold/upgrade_admin.diff")
	if err != nil {
		t.Fatal(err)
	}
	mainDiff, _, _ := strings.Cut(string(diff), "diff --git a/project/project.go")
	if !strings.Contains(guide, strings.TrimRight(mainDiff, "\n")) {
		t.Errorf("the guide's upgrade section does not show the main.go diff the upgrade test applies (testdata/legacy_scaffold/upgrade_admin.diff)")
	}

	// The anchors the CLI and the other docs link to exist.
	for _, heading := range []string{"## Adding the shell to an existing project", "## Your own helpers", "## What the interpreter cannot run", "## The helpers"} {
		if !strings.Contains(guide, "\n"+heading+"\n") {
			t.Errorf("the guide has no %q heading", heading)
		}
	}
	for _, name := range []string{"Models", "Describe", "Get", "List", "Count", "Create", "Update", "Delete", "Context"} {
		if !strings.Contains(guide, "`"+name+"(") {
			t.Errorf("the guide does not document %s()", name)
		}
	}
}
