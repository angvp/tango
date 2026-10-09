package shellcore

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// historyLimit is how many entries the history file keeps.
const historyLimit = 1000

// historyOff is the value of TANGO_SHELL_HISTORY that turns the history file off.
const historyOff = "off"

// History keeps the lines typed at the prompt, newest first for the terminal's
// up-arrow, and appends each to a file so the next session in the same project
// starts with them. It implements golang.org/x/term's History.
//
// The file can hold secrets typed into the shell, so it is private to the
// user (mode 0600, in a 0700 directory).
type History struct {
	entries []string // oldest first
	path    string   // "" when nothing is persisted
	warn    io.Writer
	warned  bool
}

// OpenHistory loads the history for the project in dir, from the user's cache
// directory. TANGO_SHELL_HISTORY=off, or a cache directory that cannot be
// used, leaves it in memory only; the second case is reported once to warn.
func OpenHistory(dir string, warn io.Writer) *History {
	h := &History{warn: warn}
	if os.Getenv("TANGO_SHELL_HISTORY") == historyOff {
		return h
	}
	path, err := historyPath(dir)
	if err != nil {
		h.complain("history is not saved: %v", err)
		return h
	}
	h.path = path
	h.load()
	return h
}

// historyPath names the history file for the project in dir: one file per
// project, under the user's cache directory.
func historyPath(dir string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(abs))
	name := filepath.Base(abs) + "-" + hex.EncodeToString(sum[:])[:12] + ".txt"
	return filepath.Join(base, "tango", "shell-history", name), nil
}

func (h *History) load() {
	file, err := os.Open(h.path)
	if err != nil {
		if !os.IsNotExist(err) {
			h.complain("history is not saved: %v", err)
			h.path = ""
		}
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			h.entries = append(h.entries, line)
		}
	}
	if len(h.entries) > historyLimit {
		h.entries = h.entries[len(h.entries)-historyLimit:]
		h.rewrite()
	}
}

// Add records a typed line. Blank lines and a repeat of the previous line are
// not kept.
func (h *History) Add(entry string) {
	entry = strings.TrimSpace(entry)
	if entry == "" || strings.ContainsAny(entry, "\n\r") {
		return
	}
	if n := len(h.entries); n > 0 && h.entries[n-1] == entry {
		return
	}
	h.entries = append(h.entries, entry)
	trimmed := len(h.entries) > historyLimit
	if trimmed {
		h.entries = h.entries[len(h.entries)-historyLimit:]
	}
	if h.path == "" {
		return
	}
	if trimmed {
		h.rewrite()
		return
	}
	h.append(entry)
}

// Len and At let the terminal walk the history, index 0 being the newest.
func (h *History) Len() int { return len(h.entries) }

func (h *History) At(idx int) string { return h.entries[len(h.entries)-1-idx] }

// Entries returns the kept lines, oldest first.
func (h *History) Entries() []string { return append([]string(nil), h.entries...) }

func (h *History) append(entry string) {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		h.fail(err)
		return
	}
	file, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		h.fail(err)
		return
	}
	defer file.Close()
	if _, err := fmt.Fprintln(file, entry); err != nil {
		h.fail(err)
	}
}

func (h *History) rewrite() {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		h.fail(err)
		return
	}
	data := strings.Join(h.entries, "\n") + "\n"
	if err := os.WriteFile(h.path, []byte(data), 0o600); err != nil {
		h.fail(err)
	}
}

// fail stops saving after a write error, telling the person once.
func (h *History) fail(err error) {
	h.complain("history is not saved: %v", err)
	h.path = ""
}

func (h *History) complain(format string, args ...any) {
	if h.warned || h.warn == nil {
		return
	}
	h.warned = true
	fmt.Fprintf(h.warn, "tango shell: "+format+"\n", args...)
}
