package shellcore

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// cacheHome points the user's cache directory at a fresh temporary one.
func cacheHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("LocalAppData", filepath.Join(home, "AppData"))
	t.Setenv("TANGO_SHELL_HISTORY", "")
	return home
}

func TestHistoryIsNewestFirstAndSkipsBlanksAndRepeats(t *testing.T) {
	cacheHome(t)
	h := OpenHistory(t.TempDir(), nil)
	for _, line := range []string{"a", "  ", "b", "b", "c", ""} {
		h.Add(line)
	}
	if h.Len() != 3 || h.At(0) != "c" || h.At(1) != "b" || h.At(2) != "a" {
		t.Fatalf("history = %v, want c b a newest first", h.Entries())
	}
}

func TestHistoryCarriesOverToTheNextSessionOfTheSameProject(t *testing.T) {
	cacheHome(t)
	project, other := t.TempDir(), t.TempDir()
	first := OpenHistory(project, nil)
	first.Add("Models()")
	first.Add(`Count("blog.Post")`)

	second := OpenHistory(project, nil)
	if got := strings.Join(second.Entries(), "|"); got != `Models()|Count("blog.Post")` {
		t.Fatalf("second session history = %q", got)
	}
	if got := OpenHistory(other, nil).Len(); got != 0 {
		t.Fatalf("another project sees %d entries, want its own empty history", got)
	}
}

func TestHistoryKeepsOnlyTheLastThousandEntries(t *testing.T) {
	cacheHome(t)
	project := t.TempDir()
	h := OpenHistory(project, nil)
	for i := 0; i < historyLimit+50; i++ {
		h.Add(fmt.Sprintf("line %d", i))
	}
	if h.Len() != historyLimit || h.At(0) != fmt.Sprintf("line %d", historyLimit+49) || h.At(historyLimit-1) != "line 50" {
		t.Fatalf("in memory: %d entries from %q to %q", h.Len(), h.At(0), h.At(historyLimit-1))
	}
	reopened := OpenHistory(project, nil)
	if reopened.Len() != historyLimit || reopened.At(historyLimit-1) != "line 50" {
		t.Fatalf("on disk: %d entries, oldest %q", reopened.Len(), reopened.At(reopened.Len()-1))
	}
}

func TestAnOverlongFileIsTrimmedWhenLoaded(t *testing.T) {
	cacheHome(t)
	project := t.TempDir()
	path, err := historyPath(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; i < historyLimit+200; i++ {
		fmt.Fprintf(&b, "old %d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	h := OpenHistory(project, nil)
	if h.Len() != historyLimit || h.At(historyLimit-1) != "old 200" {
		t.Fatalf("loaded %d entries, oldest %q, want the last %d", h.Len(), h.At(h.Len()-1), historyLimit)
	}
	data, _ := os.ReadFile(path)
	if n := strings.Count(string(data), "\n"); n != historyLimit {
		t.Fatalf("file holds %d lines after loading, want it trimmed to %d", n, historyLimit)
	}
}

func TestHistoryOffWritesNothingAndReadsNothing(t *testing.T) {
	home := cacheHome(t)
	project := t.TempDir()
	OpenHistory(project, nil).Add("kept")

	t.Setenv("TANGO_SHELL_HISTORY", "off")
	h := OpenHistory(project, nil)
	if h.Len() != 0 {
		t.Fatalf("history off still loaded %v", h.Entries())
	}
	h.Add("secret")
	if h.Len() != 1 {
		t.Fatal("history off must still work in memory for the up arrow")
	}
	t.Setenv("TANGO_SHELL_HISTORY", "")
	if got := strings.Join(OpenHistory(project, nil).Entries(), "|"); got != "kept" {
		t.Fatalf("file after an off session = %q, want only what was saved before", got)
	}
	_ = home
}

func TestHistoryFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not meaningful on Windows")
	}
	cacheHome(t)
	project := t.TempDir()
	OpenHistory(project, nil).Add("token = \"hunter2\"")
	path, _ := historyPath(project)
	file, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := os.Stat(filepath.Dir(path))
	if file.Mode().Perm() != 0o600 || dir.Mode().Perm() != 0o700 {
		t.Fatalf("file mode %v, directory mode %v, want 0600 and 0700", file.Mode().Perm(), dir.Mode().Perm())
	}
}

func TestAnUnusableHistoryDirectoryWarnsOnceAndTheShellGoesOn(t *testing.T) {
	home := cacheHome(t)
	// A file where the "tango" cache directory belongs makes it uncreatable.
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "tango"), []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	h := OpenHistory(t.TempDir(), &warn)
	h.Add("one")
	h.Add("two")
	if h.Len() != 2 {
		t.Fatalf("history = %v, want the in-memory history to keep working", h.Entries())
	}
	if n := strings.Count(warn.String(), "history is not saved"); n != 1 {
		t.Fatalf("warnings = %q, want exactly one", warn.String())
	}
	_ = home
}

// skipUnlessPermissionsApply skips a test that relies on file modes: Windows
// does not honour them and root ignores them.
func skipUnlessPermissionsApply(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file modes are not enforced here")
	}
}

// historyFileFor creates the project's history file, as an earlier session
// would have, and returns its path.
func historyFileFor(t *testing.T, project, content string) string {
	t.Helper()
	path, err := historyPath(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// keepsWorkingAfterOneWarning checks the contract of every history failure:
// the person is told once, and the session's own history still works.
func keepsWorkingAfterOneWarning(t *testing.T, h *History, warn *bytes.Buffer) {
	t.Helper()
	h.Add("one")
	h.Add("two")
	if h.At(0) != "two" || h.At(1) != "one" {
		t.Fatalf("history = %v, want the in-memory history to keep working", h.Entries())
	}
	if n := strings.Count(warn.String(), "history is not saved"); n != 1 {
		t.Fatalf("warnings = %q, want exactly one", warn.String())
	}
	if h.path != "" {
		t.Fatalf("history still saves to %q after the failure, want saving stopped", h.path)
	}
}

// neverSaved checks that the lines typed after a failure did not reach path,
// once its permissions are back so it can be read.
func neverSaved(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "one") || strings.Contains(string(data), "two") {
		t.Fatalf("history file holds lines typed after the failure: %q", data)
	}
}

func TestAnUnreadableHistoryFileWarnsOnceAndTheShellGoesOn(t *testing.T) {
	skipUnlessPermissionsApply(t)
	cacheHome(t)
	project := t.TempDir()
	path := historyFileFor(t, project, "old line\n")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	keepsWorkingAfterOneWarning(t, OpenHistory(project, &warn), &warn)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	neverSaved(t, path)
}

func TestAnUnwritableHistoryDirectoryWarnsOnceAndTheShellGoesOn(t *testing.T) {
	skipUnlessPermissionsApply(t)
	cacheHome(t)
	project := t.TempDir()
	path := historyFileFor(t, project, "")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	var warn bytes.Buffer
	keepsWorkingAfterOneWarning(t, OpenHistory(project, &warn), &warn)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	neverSaved(t, path)
}

func TestAHistoryFileThatCannotBeTrimmedWarnsOnceAndTheShellGoesOn(t *testing.T) {
	skipUnlessPermissionsApply(t)
	cacheHome(t)
	project := t.TempDir()
	var lines strings.Builder
	for i := 0; i < historyLimit+5; i++ {
		fmt.Fprintf(&lines, "line %d\n", i)
	}
	path := historyFileFor(t, project, lines.String())
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	h := OpenHistory(project, &warn)
	if h.Len() != historyLimit {
		t.Fatalf("loaded %d entries, want the last %d in memory", h.Len(), historyLimit)
	}
	keepsWorkingAfterOneWarning(t, h, &warn)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	neverSaved(t, path)
}

func TestWithNoCacheDirectoryHistoryStaysInMemoryAndWarnsOnce(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" || runtime.GOOS == "plan9" {
		t.Skip("UserCacheDir does not depend on the environment alone here")
	}
	cacheHome(t)
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	var warn bytes.Buffer
	keepsWorkingAfterOneWarning(t, OpenHistory(t.TempDir(), &warn), &warn)
}
