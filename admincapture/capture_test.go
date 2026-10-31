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
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

const (
	adminUser      = "admin"
	adminPassword  = "secret"
	readerUser     = "reader"
	readerPassword = "reader-secret"
	readerToken    = "capture-reader-session"
	adminToken     = "capture-admin-session"
	sessionCookie  = "tango_admin_session"
	stepTimeout    = 30 * time.Second
	settleTime     = 300 * time.Millisecond

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

// captureDraft has no published row, so a filter on it matches nothing and the
// changelist shows its filtered empty state.
type captureDraft struct {
	ID        int64 `tango:"pk"`
	Title     string
	Published bool
}

// captureDocument has a JSON field and a Markdown field with a preview, so the
// shots show both widgets and their tabs.
type captureDocument struct {
	ID      int64 `tango:"pk"`
	Title   string
	Body    string
	Payload string `tango:"json"`
}

// previewDocument is the capture site's Markdown renderer. The shots only need
// a heading to show that the preview tab renders the text.
func previewDocument(_ context.Context, source string) (template.HTML, error) {
	return template.HTML("<h1>Preview</h1><p>" + template.HTMLEscapeString(source) + "</p>"), nil
}

// dashboardFeed is what the capture site's status card reports. The server
// runs in this process, so a shot sets the feed before it visits the page, and
// each dashboard shot sets every field it depends on.
var dashboardFeed struct {
	sync.Mutex
	status admin.DashboardStatus
	err    error
}

func reachableStatus() admin.DashboardStatus {
	return admin.DashboardStatus{Database: admin.DatabaseReachable, Dialect: "SQLite", TrackingTable: admin.TrackingTablePresent, Release: "dev"}
}

func setDashboardFeed(status admin.DashboardStatus, err error) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(context.Context, *chromedp.Target) error {
		dashboardFeed.Lock()
		defer dashboardFeed.Unlock()
		dashboardFeed.status, dashboardFeed.err = status, err
		return nil
	})
}

// captureNote refers to a captureBook, so the photographs show a foreign key
// select with its empty choice and its add button. Copies is a number, so a
// save that submits a word for it is a field problem.
type captureNote struct {
	ID     int64 `tango:"pk"`
	Title  string
	Copies int
	BookID int64 `tango:"fk=captureBook"`
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
		for _, value := range []any{captureBook{}, captureTag{}, captureEntry{}, captureDraft{}, captureNote{}, captureDocument{}} {
			if err := registry.Models().Register(value); err != nil {
				return err
			}
		}
		if err := registry.Admin().Register(captureBook{}, admin.Options{
			ListDisplay: []string{"Title", "Pages"},
			Search:      []string{"Title"},
			BulkDelete:  true,
			Label:       "Title",
		}); err != nil {
			return err
		}
		if err := registry.Admin().Register(captureNote{}, admin.Options{
			ListDisplay: []string{"Title", "Copies"},
			Search:      []string{"Title"},
			HelpText:    map[string]string{"Copies": "Whole number of copies you hold."},
		}); err != nil {
			return err
		}
		if err := registry.Admin().Register(captureEntry{}, admin.Options{
			ListDisplay: []string{"Title", "Published"},
			Search:      []string{"Title"},
			Filters:     []string{"Published"},
			HelpText:    map[string]string{"Published": "Shown to visitors when ticked."},
		}); err != nil {
			return err
		}
		if err := registry.Admin().Register(captureDraft{}, admin.Options{
			ListDisplay: []string{"Title", "Published"},
			Search:      []string{"Title"},
			Filters:     []string{"Published"},
		}); err != nil {
			return err
		}
		if err := registry.Admin().Register(captureDocument{}, admin.Options{
			ListDisplay: []string{"Title"},
			Widgets: map[string]admin.Widget{
				"Body": admin.Markdown(admin.WithMarkdownPreview(previewDocument)),
			},
		}); err != nil {
			return err
		}
		return registry.Admin().Register(captureTag{}, admin.Options{ListDisplay: []string{"Name"}, Search: []string{"Name"}})
	})
}

func site(t *testing.T) http.Handler {
	t.Helper()
	models := model.NewRegistry()
	for _, value := range []any{admin.AdminUser{}, admin.AdminSession{}, captureBook{}, captureTag{}, captureEntry{}, captureDraft{}, captureNote{}, captureDocument{}} {
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
	// The reader account has a session but no staff access, for the access page.
	if err := admin.CreateAccount(context.Background(), store, readerUser, readerPassword); err != nil {
		t.Fatalf("seed reader account: %v", err)
	}
	if _, err := sqlDB.Exec(`UPDATE admin_user SET is_staff = ? WHERE username = ?`, false, readerUser); err != nil {
		t.Fatalf("drop staff access: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO admin_session (token, user_id, expires_at) SELECT ?, id, ? FROM admin_user WHERE username = ?`, readerToken, time.Now().Add(time.Hour).UTC(), readerUser); err != nil {
		t.Fatalf("seed reader session: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO admin_session (token, user_id, expires_at) SELECT ?, id, ? FROM admin_user WHERE username = ?`, adminToken, time.Now().Add(time.Hour).UTC(), adminUser); err != nil {
		t.Fatalf("seed admin session: %v", err)
	}
	for i := 1; i <= seededBooks; i++ {
		if _, err := sqlDB.Exec(`INSERT INTO capture_book (id, title, pages) VALUES (?, ?, ?)`, i, fmt.Sprintf("Field notes, volume %d", i), 80+i*17); err != nil {
			t.Fatalf("seed book: %v", err)
		}
	}
	for i := 1; i <= 3; i++ {
		if _, err := sqlDB.Exec(`INSERT INTO capture_draft (id, title, published) VALUES (?, ?, ?)`, i, fmt.Sprintf("Draft %d", i), false); err != nil {
			t.Fatalf("seed draft: %v", err)
		}
	}
	for i := 1; i <= seededEntries; i++ {
		if _, err := sqlDB.Exec(`INSERT INTO capture_entry (id, title, published) VALUES (?, ?, ?)`, i, fmt.Sprintf("Entry %02d", i), i%2 == 0); err != nil {
			t.Fatalf("seed entry: %v", err)
		}
	}
	if _, err := sqlDB.Exec(`INSERT INTO capture_note (id, title, copies, book_id) VALUES (1, 'Margin notes', 2, 3)`); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO capture_document (id, title, body, payload) VALUES (1, 'Field guide', '# Field guide', '{"pages": 80}')`); err != nil {
		t.Fatalf("seed document: %v", err)
	}
	registry, err := tango.BuildRegistry(tango.Config{
		InstalledApps: []tango.App{
			captureApp(),
			admin.New(store,
				admin.WithDashboard(admin.DashboardConfig{
					Status: func(context.Context) (admin.DashboardStatus, error) {
						dashboardFeed.Lock()
						defer dashboardFeed.Unlock()
						return dashboardFeed.status, dashboardFeed.err
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

// tabUntil presses Tab with the keyboard until the selector's element holds
// focus. It fails loudly when the element is never reached, so a control that
// cannot take keyboard focus cannot produce a shot of the wrong state.
func tabUntil(selector string) chromedp.Action[chromedp.Void] {
	return pressUntil(kb.Tab, selector)
}

// focusState reports whether the selector's element holds focus, and describes
// the element that does, so a failed walk can say where focus went.
type focusState struct {
	Hit  bool   `json:"hit"`
	Desc string `json:"desc"`
}

// pressUntil presses key until the selector's element holds focus, with the
// same loud failure as tabUntil. It moves through a widget's roving tab stops
// with the arrow keys the way a keyboard user does. Every failure carries the
// last focused elements, so a stuck walk and a missed wait read differently.
func pressUntil(key, selector string, opts ...chromedp.KeyOption) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
		const maxPresses = 200
		const trailLength = 10
		probe := fmt.Sprintf(`(function () {
			var e = document.activeElement;
			if (e === null) { return {hit: false, desc: "none"}; }
			return {hit: e.matches(%q), desc: e.tagName + "#" + e.id + "." + e.getAttribute("class")};
		})()`, selector)
		var trail []string
		fail := func(err error) error {
			return fmt.Errorf("%w; recent focus: %s", err, strings.Join(trail, " > "))
		}
		for i := 0; i < maxPresses; i++ {
			state, err := chromedp.Evaluate[focusState](probe)(ctx, target)
			if err != nil {
				return fail(err)
			}
			trail = append(trail, state.Desc)
			if len(trail) > trailLength {
				trail = trail[len(trail)-trailLength:]
			}
			if state.Hit {
				return nil
			}
			if _, err := chromedp.KeyEvent(key, opts...)(ctx, target); err != nil {
				return fail(err)
			}
		}
		return fail(fmt.Errorf("keyboard focus did not reach %s within %d presses", selector, maxPresses))
	})
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
		return []chromedp.Action[chromedp.Void]{setDashboardFeed(reachableStatus(), nil), visit(b, "/admin/")}
	}},
	// One dashboard shot per badge variant and the warning alert. The neutral
	// alert needs a site with no status source, which this site does not have.
	{name: "dashboard-badge-danger", ready: "[data-dashboard]", steps: func(b string) []chromedp.Action[chromedp.Void] {
		unreachable := reachableStatus()
		unreachable.Database = admin.DatabaseUnreachable
		return []chromedp.Action[chromedp.Void]{setDashboardFeed(unreachable, nil), visit(b, "/admin/")}
	}},
	{name: "dashboard-badge-neutral", ready: "[data-dashboard]", steps: func(b string) []chromedp.Action[chromedp.Void] {
		unknown := reachableStatus()
		unknown.Database = admin.DatabaseUnknown
		return []chromedp.Action[chromedp.Void]{setDashboardFeed(unknown, nil), visit(b, "/admin/")}
	}},
	{name: "dashboard-badge-warning", ready: "[data-dashboard]", steps: func(b string) []chromedp.Action[chromedp.Void] {
		pending := reachableStatus()
		pending.Pending = []string{"0002_add_year", "0003_add_price"}
		return []chromedp.Action[chromedp.Void]{setDashboardFeed(pending, nil), visit(b, "/admin/")}
	}},
	{name: "dashboard-badge-tracking-missing", ready: "[data-dashboard]", steps: func(b string) []chromedp.Action[chromedp.Void] {
		missing := reachableStatus()
		missing.TrackingTable = admin.TrackingTableMissing
		return []chromedp.Action[chromedp.Void]{setDashboardFeed(missing, nil), visit(b, "/admin/")}
	}},
	{name: "dashboard-alert-warning", ready: "[data-dashboard]", steps: func(b string) []chromedp.Action[chromedp.Void] {
		// The injected failure is reset once the warning page has loaded, so no
		// later /admin/ load in the shared process fails its status card.
		return []chromedp.Action[chromedp.Void]{
			setDashboardFeed(admin.DashboardStatus{}, context.DeadlineExceeded),
			visit(b, "/admin/"),
			setDashboardFeed(reachableStatus(), nil),
		}
	}},
	{name: "filter-no-match", ready: ".content-body", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_draft/?filter_published=yes")}
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
	// The error pages a person reaches by following a link or submitting a
	// form. Their ready selector is the sentence each page shows.
	{name: "error-expired-form", ready: "main p.text-base", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_book/new/"),
			chromedp.WaitVisible("main form[method=post]"),
			script(`(() => { const form = document.querySelector('main form[method=post]'); form.querySelector('input[name=csrf_token]').remove(); form.submit(); })()`),
		}
	}},
	{name: "error-not-found", ready: "main p.text-base", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_book/999999/")}
	}},
	// The access page for an account without staff access. The browser's own
	// session is swapped for the reader's for this load, then restored, so the
	// shots after it keep the admin session.
	{name: "error-no-staff-access", ready: "main p.text-base", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
				_, err := chromedp.Call(c, network.SetCookie, network.SetCookieParams{
					Name: sessionCookie, Value: readerToken, URL: b + "/admin/",
				})
				return err
			}),
			visit(b, "/admin/capture_book/"),
			chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
				_, err := chromedp.Call(c, network.SetCookie, network.SetCookieParams{
					Name: sessionCookie, Value: adminToken, URL: b + "/admin/",
				})
				return err
			}),
		}
	}},
	{name: "fk-create-form", ready: "select#field-BookID", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_note/new/")}
	}},
	{name: "fk-edit-form", ready: "select#field-BookID", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_note/1/")}
	}},
	{name: "create-form-failed-save-summary", ready: "#form-summary", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_note/new/"),
			chromedp.WaitVisible("#field-Copies"),
			script(`document.getElementById('field-Copies').type = 'text'`),
			script(`var book = document.getElementById('field-BookID'); book.add(new Option('x', 'x')); book.value = 'x'`),
			chromedp.SendKeys("#field-Title", "Two problems"),
			chromedp.SendKeys("#field-Copies", "many"),
			chromedp.Submit("#field-Title"),
		}
	}},
	{name: "entry-create-form", ready: "form", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_entry/new/")}
	}},
	{name: "widget-json-edit", ready: "#field-Payload", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_document/1/")}
	}},
	{name: "widget-json-formatted", ready: "[data-json-status].field-error", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_document/1/"),
			script(`document.getElementById('field-Payload').value = '{"pages": 80,}'; document.getElementById('field-Payload-tab-view').click();`),
		}
	}},
	{name: "widget-markdown-write", ready: "#field-Body", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_document/1/")}
	}},
	{name: "widget-markdown-preview", ready: "#field-Body-preview[srcdoc]", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_document/1/"),
			script(`document.getElementById('field-Body-tab-preview').click();`),
		}
	}},
	{name: "widget-json-server-error", ready: ".field-error", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_document/1/"),
			script(`document.getElementById('field-Payload').value = '{"pages": 80,}'; document.querySelector('form[method=post] button.btn-primary').click();`),
			chromedp.WaitVisible(".field-error"),
		}
	}},
	// Keyboard focus shots. Each reaches its control by real Tab presses from the
	// top of the page, so the ring shown is the one a keyboard user sees.
	{name: "focus-nav-link", desktopOnly: true, ready: ".sidebar-link:focus-visible", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/"), tabUntil(".sidebar-link")}
	}},
	// Below md the sidebar is a drawer whose links are not in the Tab order while
	// it is closed. A keyboard user opens it with the menu button, then tabs in.
	{name: "focus-nav-link-phone", phoneOnly: true, ready: ".sidebar-link:focus-visible", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/"),
			tabUntil(".menu-btn"),
			chromedp.KeyEvent(kb.Enter),
			tabUntil(".sidebar-link"),
		}
	}},
	{name: "focus-text-input", ready: "input[name=q]:focus-visible", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_book/"), tabUntil("input[name=q]")}
	}},
	{name: "focus-primary-button", desktopOnly: true, ready: "form .btn-primary:focus-visible", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_book/1/"), tabUntil("form .btn-primary")}
	}},
	{name: "focus-select", desktopOnly: true, ready: "select#field-BookID:focus-visible", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_note/new/"), tabUntil("select#field-BookID")}
	}},
	{name: "focus-widget-tab", desktopOnly: true, ready: "#field-Payload-tab-view:focus-visible", steps: func(b string) []chromedp.Action[chromedp.Void] {
		// The tabs are a roving set: Tab reaches the selected tab, and the arrow key
		// moves to the next one.
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_document/1/"),
			tabUntil("#field-Payload-tab-edit"),
			pressUntil(kb.ArrowRight, "#field-Payload-tab-view"),
		}
	}},
	{name: "focus-bulk-bar", desktopOnly: true, ready: "[data-bulk-open]:focus-visible", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			visit(b, "/admin/capture_book/"),
			chromedp.Click(".table-admin tbody tr:nth-child(1) input[data-bulk-pk]", chromedp.NodeVisible),
			// The bar sits above the table, so Shift+Tab walks back from the row to it.
			pressUntil(kb.Tab, "[data-bulk-open]", chromedp.KeyModifiers(chromedp.ModifierShift)),
		}
	}},
	{name: "focus-row-action", desktopOnly: true, ready: ".table-admin tbody tr:nth-child(1) a:focus-visible", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{visit(b, "/admin/capture_book/"), tabUntil(".table-admin tbody tr:nth-child(1) a")}
	}},
	// Script execution is off for this shot only; the compact shot below turns it
	// back on before it runs.
	{name: "widget-json-no-script", ready: "#field-Payload", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
				_, err := chromedp.Call(c, emulation.SetScriptExecutionDisabled, emulation.SetScriptExecutionDisabledParams{Value: true})
				return err
			}),
			visit(b, "/admin/capture_document/1/"),
		}
	}},
	// Compact density is the last shot: its cookie outlives this step in the
	// shared browser, so no later shot may depend on the default density.
	{name: "changelist-rows-compact", ready: ".table-admin tbody tr", steps: func(b string) []chromedp.Action[chromedp.Void] {
		return []chromedp.Action[chromedp.Void]{
			chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
				_, err := chromedp.Call(c, emulation.SetScriptExecutionDisabled, emulation.SetScriptExecutionDisabledParams{Value: false})
				return err
			}),
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
		if s.desktopOnly && vp.mobile {
			rows = append(rows, row{page: s.name, theme: theme, width: vp.name, captured: "n/a", notes: "this focus shot is taken at the desktop width only"})
			continue
		}
		take(s.name, s.ready, s.steps(base)...)
	}
	return rows
}
