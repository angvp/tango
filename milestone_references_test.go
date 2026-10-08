package tango_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// milestoneReference matches the private planning label: the word
// followed by a number.
// It is case-sensitive: the word "milestone" in ordinary prose is fine.
var milestoneReference = regexp.MustCompile(`Milestone [0-9]`)

// TestNoInternalMilestoneReferences keeps the private roadmap's milestone
// numbers out of everything published from this repository: code, tests,
// examples, docs, and CLI output. Name the feature or cite the ADR instead.
func TestNoInternalMilestoneReferences(t *testing.T) {
	var found []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(content, 0) >= 0 {
			return nil // binary
		}
		for i, line := range strings.Split(string(content), "\n") {
			if milestoneReference.MatchString(line) {
				found = append(found, fmt.Sprintf("%s:%d: %s", path, i+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	if len(found) > 0 {
		t.Fatalf("%d internal milestone reference(s); name the feature or cite the ADR instead:\n%s",
			len(found), strings.Join(found, "\n"))
	}
}
