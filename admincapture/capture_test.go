// Package admincapture photographs the admin's pages in light and dark at a
// desktop and a phone width, so a change to the admin's look has a before
// picture to compare against. It is its own Go module, so the root module's
// ./... does not build it and the root module gains no dependency.
//
// It does nothing unless TANGO_CAPTURE_DIR names an output directory outside
// the repository. Run it from this directory:
//
//	TANGO_CAPTURE_DIR=/path/outside/the/repo go test -run TestCaptureAdminPages -count=1 -v ./...
//
// TANGO_BROWSER_PATH names a Chrome or Chromium binary explicitly. The run
// writes one PNG per page, theme and width, and checklist.md, whose
// Captured column is filled from the files that exist.
package admincapture_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

const (
	adminUser     = "admin"
	adminPassword = "secret"
	stepTimeout   = 30 * time.Second
	settleTime    = 300 * time.Millisecond

	// seededBooks fills the changelist past a phone screen yet stays within one
	// page of the admin, so the photographs show a populated list without
	// pagination controls.
	seededBooks = 14
)

type captureBook struct {
	ID    int64 `tango:"pk"`
	Title string
	Pages int
}

// captureEntry is a second model with a boolean the changelist filters on and
// enough rows to run to a second page. It is separate from captureBook, so the
// photographs of the books list keep their fixed rows.
type captureEntry struct {
	ID        int64 `tango:"pk"`
	Title     string
	Published bool
}

// seededEntries is more than one page of entries, and half of them are
// published, so a filter leaves a page that still shows its summary.
const seededEntries = 30

type captureTag struct {
	ID   int64 `tango:"pk"`
	Name string
}

func captureApp() tango.App {
	return tango.NewApp("capture", func(registry *tango.Registry) error {
		for _, value := range []any{captureBook{}, captureTag{}, captureEntry{}} {
			if err := registry.Models().Register(value); err != nil {
				return err
			}
		}
		if err := registry.Admin().Register(captureBook{}, admin.Options{
			ListDisplay: []string{"Title", "Pages"},
			Search:      []string{"Title"},
			BulkDelete:  true,
		}); err != nil {
			return err
		}
		if err := registry.Admin().Register(captureEntry{}, admin.Options{
			ListDisplay: []string{"Title", "Published"},
			Search:      []string{"Title"},
			Filters:     []string{"Published"},
		}); err != nil {
			return err
		}
		return registry.Admin().Register(captureTag{}, admin.Options{ListDisplay: []string{"Name"}, Search: []string{"Name"}})
	})
}

func site(t *testing.T) http.Handler {
	t.Helper()
	models := model.NewRegistry()
	for _, value := range []any{admin.AdminUser{}, admin.AdminSession{}, captureBook{}, captureTag{}, captureEntry{}} {
		if err := models.Register(value); err != nil {
			t.Fatalf("register %T: %v", value, err)
		}
	}
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, models.All())
	store := db.NewStore(sqlDB, dialect)
	if err := admin.CreateAccount(context.Background(), store, adminUser, adminPassword); err != nil {
		t.Fatalf("seed admin account: %v", err)
	}
	for i := 1; i <= seededBooks; i++ {
		if _, err := sqlDB.Exec(`INSERT INTO capture_book (id, title, pages) VALUES (?, ?, ?)`, i, fmt.Sprintf("Field notes, volume %d", i), 80+i*17); err != nil {
			t.Fatalf("seed book: %v", err)
		}
	}
	for i := 1; i <= seededEntries; i++ {
		if _, err := sqlDB.Exec(`INSERT INTO capture_entry (id, title, published) VALUES (?, ?, ?)`, i, fmt.Sprintf("Entry %02d", i), i%2 == 0); err != nil {
			t.Fatalf("seed entry: %v", err)
		}
	}
	registry, err := tango.BuildRegistry(tango.Config{
		InstalledApps: []tango.App{
			captureApp(),
			admin.New(store,
				admin.WithDashboard(admin.DashboardConfig{
					Status: func(context.Context) (admin.DashboardStatus, error) {
						return admin.DashboardStatus{Database: admin.DatabaseReachable, Dialect: "SQLite", TrackingTable: admin.TrackingTablePresent, Release: "dev"}, nil
					},
				}),
				admin.WithMiddleware(tango.SecurityHeaders(tango.WithContentSecurityPolicy(admin.ContentSecurityPolicy()))),
			),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func chromeBinary(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("TANGO_BROWSER_PATH"); path != "" {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("TANGO_BROWSER_PATH=%q: %v", path, err)
		}
		return path
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	const macChrome = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if _, err := os.Stat(macChrome); err == nil {
		return macChrome
	}
	return ""
}

type viewport struct {
	name          string
	width, height int64
	mobile        bool
}

var viewports = []viewport{
	{"1440x900", 1440, 900, false},
	{"390x844", 390, 844, true},
}

var themes = []string{"light", "dark"}

// shot is one page to photograph: the steps that bring the browser to it and
// the selector that must be visible before the photograph.
type shot struct {
	name        string
	desktopOnly bool
	phoneOnly   bool
	steps       func(base string) []chromedp.Action[chromedp.Void]
	ready       string
}

// script runs a statement in the page and discards its result.
func script(source string) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
		_, err := chromedp.Evaluate[chromedp.Void](source)(ctx, target)
		return err
	})
}

func visit(base, path string) chromedp.Action[chromedp.Void] {
	return chromedp.Navigate(base + path)
}

// signedInShots are taken after sign-in; the login page is taken before.
var signedInShots = []shot{
	{name: "changelist-rows", ready: ".table-admin tbody tr", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_book/")}
	}},
	{name: "changelist-empty", ready: ".content-body", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_tag/")}
	}},
	{name: "search-no-match", ready: ".content-body", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_book/?q=zzzzzz")}
	}},
	{name: "create-form", ready: "form", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_book/new/")}
	}},
	{name: "create-form-failed-save", ready: ".field-error", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_book/new/"),
			chromedp.WaitVisible("#field-Pages"),
			script(`document.getElementById('field-Pages').type = 'text'`),
			chromedp.SendKeys("#field-Title", "A title"),
			chromedp.SendKeys("#field-Pages", "not a number"),
			chromedp.Submit("#field-Title"),
		}
	}},
	{name: "edit-form", ready: "form", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_book/1/")}
	}},
	{name: "delete-confirmation", ready: "form", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_book/1/delete/")}
	}},
	{name: "bulk-dialog-open", ready: "dialog[open]", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_book/"),
			chromedp.Click("[data-bulk-pk]", chromedp.NodeVisible),
			chromedp.Click("[data-bulk-open]", chromedp.NodeVisible),
		}
	}},
	{name: "dashboard", ready: "[data-dashboard]", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/")}
	}},
	{name: "phone-drawer-closed", phoneOnly: true, ready: ".menu-btn", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_book/")}
	}},
	{name: "phone-drawer-open", phoneOnly: true, ready: ".sidebar-open", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_book/"),
			chromedp.Click(".menu-btn", chromedp.NodeVisible),
		}
	}},
	{name: "search-with-clear", ready: ".content-body", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_book/?q=volume")}
	}},
	{name: "search-one-row", ready: ".content-body", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_book/?q=volume+14")}
	}},
	{name: "entries-filtered", ready: ".table-admin tbody tr", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_entry/?q=Entry&filter_published=yes")}
	}},
	{name: "entries-paged", ready: ".pagination-pages", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_entry/?page=2")}
	}},
	// A save of the unchanged edit form redirects with the saved outcome, so the
	// status region carries a message without changing any row.
	{name: "status-message-after-save", ready: ".status-message", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_book/1/"),
			chromedp.WaitVisible("#field-Title"),
			chromedp.Submit("#field-Title"),
		}
	}},
	{name: "bulk-bar-selected", ready: "[data-bulk-open]:not([disabled])", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_book/"),
			chromedp.Click(".table-admin tbody tr:nth-child(1) input[data-bulk-pk]", chromedp.NodeVisible),
		}
	}},
	// The selection names a row that no longer exists, so the server refuses the
	// whole action and shows the rejection page. The key is rewritten in the page,
	// the only way a browser can send a stale selection.
	{name: "bulk-rejection", ready: ".alert-error", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_book/"),
			chromedp.Click(".table-admin tbody tr:nth-child(1) input[data-bulk-pk]", chromedp.NodeVisible),
			chromedp.Click("[data-bulk-open]", chromedp.NodeVisible),
			script(`document.querySelectorAll('[data-bulk-form] input[name=pk]').forEach(function (i) { i.value = '999999'; })`),
			chromedp.Click("[data-bulk-confirm]", chromedp.NodeVisible),
		}
	}},
	// Compact density is the last shot: its cookie outlives this step in the
	// shared browser, so no later shot may depend on the default density.
	{name: "changelist-rows-compact", ready: ".table-admin tbody tr", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
				_, err := chromedp.Call(c, network.SetCookie, network.SetCookieParams{
					Name: "tango_admin_density", Value: "compact", URL: b + "/admin/",
				})
				return err
			}),
			visit(b, "/admin/capture_book/"),
		}
	}},
}

type row struct{ page, theme, width, captured, notes string }

func TestCaptureAdminPages(t *testing.T) {
	outDir := os.Getenv("TANGO_CAPTURE_DIR")
	if outDir == "" {
		t.Skip("TANGO_CAPTURE_DIR is not set; the capture did not run")
	}
	outDir, err := filepath.Abs(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := chromeBinary(t)
	if binary == "" {
		t.Fatal("no Chrome or Chromium found; set TANGO_BROWSER_PATH")
	}

	server := httptest.NewServer(site(t))
	t.Cleanup(server.Close)

	var rows []row
	for _, theme := range themes {
		for _, vp := range viewports {
			rows = append(rows, captureCombination(t, binary, server.URL, outDir, theme, vp)...)
		}
	}

	var table strings.Builder
	table.WriteString("| Page | Theme | Width | Captured | Notes |\n|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&table, "| %s | %s | %s | %s | %s |\n", r.page, r.theme, r.width, r.captured, r.notes)
	}
	if err := os.WriteFile(filepath.Join(outDir, "checklist.md"), []byte(table.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func captureCombination(t *testing.T, binary, base, outDir, theme string, vp viewport) []row {
	t.Helper()
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(binary), chromedp.WindowSize(int(vp.width), int(vp.height)))...)
	defer cancelAllocator()
	ctx, cancel := chromedp.NewContext(allocator)
	defer cancel()

	// The first call on a context starts the browser, so it runs without a deadline.
	if err := chromedp.Do(ctx); err != nil {
		t.Fatalf("start browser: %v", err)
	}
	do := func(steps ...chromedp.Action[chromedp.Void]) error {
		stepCtx, stepCancel := context.WithTimeout(ctx, stepTimeout)
		defer stepCancel()
		return chromedp.Do(stepCtx, steps...)
	}
	if err := do(
		chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
			_, err := chromedp.Call(c, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
				Width: vp.width, Height: vp.height, DeviceScaleFactor: 1, Mobile: vp.mobile,
			})
			return err
		}),
		chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
			_, err := chromedp.Call(c, network.SetCookie, network.SetCookieParams{
				Name: "tango_admin_theme", Value: theme, URL: base + "/admin/",
			})
			return err
		}),
	); err != nil {
		t.Fatalf("%s %s setup: %v", theme, vp.name, err)
	}

	var rows []row
	take := func(name, ready string, steps ...chromedp.Action[chromedp.Void]) {
		file := fmt.Sprintf("%s-%s-%s.png", name, theme, vp.name)
		entry := row{page: name, theme: theme, width: vp.name, captured: "no"}
		var png []byte
		all := append(append([]chromedp.Action[chromedp.Void]{}, steps...),
			chromedp.WaitVisible(ready),
			chromedp.Sleep(settleTime),
			chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
				var err error
				png, err = chromedp.FullScreenshot(100)(ctx, target)
				return err
			}),
		)
		if err := do(all...); err != nil {
			entry.notes = err.Error()
		} else if len(png) == 0 {
			entry.notes = "empty screenshot"
		} else if err := os.WriteFile(filepath.Join(outDir, file), png, 0o644); err != nil {
			entry.notes = err.Error()
		} else {
			entry.captured = "yes"
		}
		if entry.captured != "yes" {
			t.Errorf("%s %s %s: not captured: %s", name, theme, vp.name, entry.notes)
		}
		rows = append(rows, entry)
	}

	take("login", "form", visit(base, "/admin/login/"))
	if err := do(
		chromedp.SendKeys("#field-username", adminUser),
		chromedp.SendKeys("#field-password", adminPassword),
		chromedp.Submit("#field-password"),
		chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
			for {
				path, err := chromedp.Evaluate[string](`location.pathname`)(ctx, target)
				if err != nil {
					return err
				}
				if !strings.HasPrefix(path, "/admin/login") {
					return nil
				}
				if _, err := chromedp.Sleep(100*time.Millisecond)(ctx, target); err != nil {
					return err
				}
			}
		}),
	); err != nil {
		t.Fatalf("%s %s sign-in: %v", theme, vp.name, err)
	}
	for _, s := range signedInShots {
		if s.phoneOnly && !vp.mobile {
			rows = append(rows, row{page: s.name, theme: theme, width: vp.name, captured: "n/a", notes: "the drawer exists only below the md breakpoint"})
			continue
		}
		take(s.name, s.ready, s.steps(base)...)
	}
	return rows
}
