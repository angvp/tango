// Package browsersmoke loads the framework's pages in a real Chrome under the
// Content-Security-Policy each one is served with, and reads the violation
// events the browser reports. It is the browser half of the CSP proof; the
// structural scan in the root module is the other half.
//
// The test skips when no Chrome or Chromium is found, and fails instead when
// TANGO_REQUIRE_BROWSER=1, which the CI job sets. TANGO_BROWSER_PATH names a
// browser binary explicitly.
package browsersmoke_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/mail/mailtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/openapi"
	"github.com/angvp/tango/openapi/swaggerui"
	"github.com/angvp/tango/testdb"
)

const (
	smokeAdminUser     = "admin"
	smokeAdminPassword = "secret"
	smokeRequestHost   = "example.com"
	// mobileWidth and mobileHeight put the admin in the layout where the menu
	// button and the sidebar backdrop exist (below the md breakpoint).
	mobileWidth  = 390
	mobileHeight = 844
	// settleTime gives asynchronous violation events time to arrive before a
	// page is judged.
	settleTime = 300 * time.Millisecond
)

// The headers each page must carry. The application policy is the strict
// reference; the admin tier replaces it on admin paths; the Swagger UI page
// replaces it with its route policy, which relaxes style-src, extends img-src
// with data:, and extends connect-src with 'self' so the page can fetch its
// document.
const (
	appHeader   = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; form-action 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"
	adminHeader = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; form-action 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"
	docsHeader  = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; form-action 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"
)

// strictReferencePolicy is the application policy. It mirrors the one the
// framework page audit uses, written here so the admin's policy value cannot
// relax the check it is measured by.
func strictReferencePolicy() tango.CSP {
	return tango.CSP{
		DefaultSrc:     []tango.Source{tango.None()},
		ScriptSrc:      []tango.Source{tango.Self()},
		StyleSrc:       []tango.Source{tango.Self()},
		ImgSrc:         []tango.Source{tango.Self()},
		FormAction:     []tango.Source{tango.Self()},
		ObjectSrc:      []tango.Source{tango.None()},
		BaseURI:        []tango.Source{tango.Self()},
		FrameAncestors: []tango.Source{tango.None()},
	}
}

// smokeNote is the one model the admin lists, edits and previews.
type smokeNote struct {
	ID      int64 `tango:"pk"`
	Title   string
	Body    string
	Pinned  bool
	Payload string `tango:"json"`
}

// smokeCode has two text fields that shortCodeWidget bounds, so a save with
// both too long is two problems and shows the form summary.
type smokeCode struct {
	ID    int64 `tango:"pk"`
	Code  string
	Label string
}

// shortCodeWidget is a host text widget whose Parse refuses a value longer
// than three characters with a field message, so two such fields are two
// problems on one save.
type shortCodeWidget struct{}

func (shortCodeWidget) Render(_ context.Context, f admin.FieldContext) template.HTML {
	invalid := ""
	if f.Error != "" {
		invalid = ` aria-invalid="true"`
	}
	return template.HTML(`<label for="field-` + f.Name + `" class="field-label">` + template.HTMLEscapeString(f.Label) + `</label>` +
		`<input id="field-` + f.Name + `" name="` + f.Name + `" value="` + template.HTMLEscapeString(f.Value) + `" class="input"` + invalid + `>`)
}

func (shortCodeWidget) Parse(_ context.Context, f admin.FieldContext, form admin.FieldValues, dest reflect.Value) error {
	value := form.Get(f.Name)
	if len(value) > 3 {
		return admin.FieldError{Message: f.Label + " must be at most 3 characters."}
	}
	dest.SetString(value)
	return nil
}

// previewNote is the host's Markdown renderer for the smoke. It emits a
// heading, so the sandboxed frame has an element the test can read.
func previewNote(_ context.Context, source string) (template.HTML, error) {
	return template.HTML("<h1>" + template.HTMLEscapeString(source) + "</h1>"), nil
}

func notesApp() tango.App {
	return tango.NewApp("notes", func(registry *tango.Registry) error {
		if err := registry.Models().Register(smokeCode{}); err != nil {
			return err
		}
		if err := registry.Admin().Register(smokeCode{}, admin.Options{
			Widgets: map[string]admin.Widget{"Code": shortCodeWidget{}, "Label": shortCodeWidget{}},
		}); err != nil {
			return err
		}
		if err := registry.Models().Register(smokeNote{}); err != nil {
			return err
		}
		return registry.Admin().Register(smokeNote{}, admin.Options{
			ListDisplay: []string{"Title"},
			Search:      []string{"Title"},
			Filters:     []string{"Pinned"},
			BulkDelete:  true,
			Widgets: map[string]admin.Widget{
				"Body":    admin.Markdown(admin.WithMarkdownPreview(previewNote)),
				"Payload": admin.JSON(),
			},
		})
	})
}

// composedSite builds the application the way the framework page audit does:
// the strict reference policy for the whole application, the admin's own
// policy on its tier, and the Swagger UI under its route policy.
func composedSite(t *testing.T) http.Handler {
	t.Helper()
	models := model.NewRegistry()
	for _, value := range []any{
		admin.AdminUser{}, admin.AdminSession{},
		accounts.Account{}, accounts.AccountSession{}, accounts.AccountToken{},
		smokeNote{}, smokeCode{},
	} {
		if err := models.Register(value); err != nil {
			t.Fatalf("register %T: %v", value, err)
		}
	}
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, models.All())
	store := db.NewStore(sqlDB, dialect)
	if err := admin.CreateAccount(context.Background(), store, smokeAdminUser, smokeAdminPassword); err != nil {
		t.Fatalf("seed admin account: %v", err)
	}
	// The second title is one unbroken word, wider than a phone screen, so a
	// table that does not wrap its cells is caught by the phone check.
	if _, err := sqlDB.Exec(`INSERT INTO smoke_note (id, title, body, pinned, payload) VALUES (1, 'Hello', 'Body', FALSE, ''), (2, 'Unbrokenphonecheckvalue0123456789abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJ', 'Body', TRUE, '')`); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	// More rows than one page, so the changelist shows its pagination at the
	// phone width.
	for id := 3; id <= 28; id++ {
		if _, err := sqlDB.Exec(`INSERT INTO smoke_note (id, title, body, pinned, payload) VALUES ($1, $2, 'Body', FALSE, '')`, id, fmt.Sprintf("Note %02d", id)); err != nil {
			t.Fatalf("seed note %d: %v", id, err)
		}
	}

	config := tango.Config{
		InstalledApps: []tango.App{
			notesApp(),
			accounts.New(store, accounts.WithMail(accounts.MailConfig{
				Sender:  &mailtest.Sender{},
				From:    "Shop <noreply@example.com>",
				BaseURL: "https://" + smokeRequestHost,
			})),
			openapi.New(openapi.Options{
				UI:   swaggerui.New(swaggerui.WithContentSecurityPolicy(strictReferencePolicy())),
				Path: "/openapi.json",
			}),
			admin.New(store,
				admin.WithDashboard(admin.DashboardConfig{
					Status: func(context.Context) (admin.DashboardStatus, error) { return admin.DashboardStatus{}, nil },
				}),
				admin.WithMiddleware(tango.SecurityHeaders(tango.WithContentSecurityPolicy(admin.ContentSecurityPolicy()))),
			),
		},
		Middleware:      []tango.Middleware{tango.SecurityHeaders(tango.WithContentSecurityPolicy(strictReferencePolicy()))},
		MiddlewareScope: tango.MiddlewareScopeAll,
	}
	registry, err := tango.BuildRegistry(config)
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

// chromeBinary returns the browser to run, or "" when none is installed.
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

// finding is one thing the browser reported that the test does not accept:
// finding is one thing the browser reported that the test does not accept:
// a CSP violation, a browser security log entry, or a request to another origin.
type finding struct {
	page   string
	source string
	detail string
}

// violationEvent is the payload the injected securitypolicyviolation listener
// logs for each event in the top document.
type violationEvent struct {
	Directive string `json:"directive"`
	Blocked   string `json:"blocked"`
	Document  string `json:"document"`
}

// recorder collects what the browser reports. The event subscriptions are
// consumed on their own goroutines, so every field is guarded by mu.
type recorder struct {
	mu       sync.Mutex
	page     string
	origin   string
	findings []finding
}

func (r *recorder) begin(page string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.page = page
}

func (r *recorder) add(source, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.findings = append(r.findings, finding{page: r.page, source: source, detail: detail})
}

func (r *recorder) sameOrigin(raw string) bool {
	for _, prefix := range []string{"data:", "blob:", "about:"} {
		if strings.HasPrefix(raw, prefix) {
			return true
		}
	}
	return strings.HasPrefix(raw, r.origin+"/") || raw == r.origin
}

// console reads the reporter script's violation messages.
func (r *recorder) console(ev runtime.EventConsoleAPICalled) {
	if len(ev.Args) < 1 {
		return
	}
	var text string
	if err := json.Unmarshal(ev.Args[0].Value, &text); err != nil {
		return
	}
	payload, ok := strings.CutPrefix(text, "TANGO_CSP ")
	if !ok {
		return
	}
	var event violationEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		r.add("securitypolicyviolation", text)
		return
	}
	r.add("securitypolicyviolation", fmt.Sprintf("directive %s blocked %s in %s", event.Directive, event.Blocked, event.Document))
}

// consume drains one event subscription until its context ends. An error
// other than cancellation means the browser stopped delivering events, which
// the test must not read as a clean page.
func consume[E any](wg *sync.WaitGroup, rec *recorder, seq iter.Seq2[E, error], handle func(E)) {
	wg.Go(func() {
		for ev, err := range seq {
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					rec.add("browser", err.Error())
				}
				return
			}
			handle(ev)
		}
	})
}

// reporterScript runs in every document the browser creates. It reports each
// violation in the document it runs in through the console. A sandboxed frame
// without allow-scripts runs no script, so violations inside one reach the test
// through the Log security entries instead.
const reporterScript = `
document.addEventListener('securitypolicyviolation', function (e) {
  console.error('TANGO_CSP ' + JSON.stringify({
    directive: e.effectiveDirective, blocked: e.blockedURI, document: location.href
  }));
});
`

// stepTimeout bounds one visit or action. A missing element fails the test
// instead of waiting for the whole browser session.
const stepTimeout = 30 * time.Second

// smoke drives one browser session through the pages.
type smoke struct {
	t    *testing.T
	ctx  context.Context
	base string
	rec  *recorder
	// frameCtx is attached to the preview frame's target on first use.
	frameCtx context.Context
}

// do runs steps under a deadline derived from the session context. The
// session context itself carries no deadline, because the first call on it
// starts the browser.
func (s *smoke) do(steps ...chromedp.Action[chromedp.Void]) error {
	ctx, cancel := context.WithTimeout(s.ctx, stepTimeout)
	defer cancel()
	return chromedp.Do(ctx, steps...)
}

func (s *smoke) settle() {
	s.t.Helper()
	if err := s.do(chromedp.Sleep(settleTime)); err != nil {
		s.t.Fatalf("settle: %v", err)
	}
}

// visit loads one path, runs actions on it, and attributes everything the
// browser reports to label until the next visit.
func (s *smoke) visit(label, path string, actions ...chromedp.Action[chromedp.Void]) {
	s.t.Helper()
	s.t.Logf("%s: GET %s", label, path)
	s.rec.begin(label)
	steps := append([]chromedp.Action[chromedp.Void]{chromedp.Navigate(s.base + path), chromedp.WaitReady("body")}, actions...)
	if err := s.do(steps...); err != nil {
		s.t.Fatalf("%s (%s): %v", label, path, err)
	}
	s.settle()
}

func (s *smoke) run(label string, steps ...chromedp.Action[chromedp.Void]) {
	s.t.Helper()
	s.t.Logf("%s", label)
	s.rec.begin(label)
	if err := s.do(steps...); err != nil {
		s.t.Fatalf("%s: %v", label, err)
	}
	s.settle()
}

func (s *smoke) eval(expr string) bool {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(s.ctx, stepTimeout)
	defer cancel()
	ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](expr))
	if err != nil {
		s.t.Fatalf("evaluate %q: %v", expr, err)
	}
	return ok
}

// positiveControl proves the instruments see a violation in this browser. It
// injects an inline script the strict policy refuses and checks that the page
// reported it through both the listener and the Log security entries. The
// control's findings are then discarded, so only the real pages are judged.
func (s *smoke) positiveControl() {
	s.visit("positive control", "/accounts/login/")
	ctx, cancel := context.WithTimeout(s.ctx, stepTimeout)
	defer cancel()
	ran, err := chromedp.Run(ctx, chromedp.Evaluate[bool](
		`(function () {
			var s = document.createElement('script');
			s.textContent = 'window.tangoControl = 1';
			document.body.appendChild(s);
			return window.tangoControl === 1;
		})()`))
	if err != nil {
		s.t.Fatalf("positive control: %v", err)
	}
	if ran {
		s.t.Fatal("the strict policy let an inline script run; the control cannot prove the instrument")
	}
	s.settle()

	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	var listener, logged bool
	for _, f := range s.rec.findings {
		if f.page != "positive control" {
			continue
		}
		if f.source == "securitypolicyviolation" && strings.Contains(f.detail, "script-src") {
			listener = true
		}
		if f.source == "browser log" {
			logged = true
		}
	}
	if !listener || !logged {
		s.t.Fatalf("positive control not seen: securitypolicyviolation listener=%v, browser log=%v; findings=%+v",
			listener, logged, s.rec.findings)
	}
	s.rec.findings = nil
}

// sidebarOpen reports whether the admin sidebar carries its open class.
func (s *smoke) sidebarOpen() bool {
	return s.eval(`document.getElementById('sidebar').classList.contains('sidebar-open')`)
}

// check reports a failed assertion without stopping the run, so independent
// expectations of one interaction are all visible in a single run.
func (s *smoke) check(ok bool, format string, args ...any) {
	s.t.Helper()
	if !ok {
		s.t.Errorf(format, args...)
	}
}

const (
	drawerVisibleOnScreen = `(function () {
		var sidebar = document.getElementById('sidebar');
		var box = sidebar.getBoundingClientRect();
		return getComputedStyle(sidebar).visibility === 'visible' && box.left >= 0 && box.right > 0 && box.width > 0;
	})()`
	focusOnMenuButton  = `document.activeElement === document.querySelector('.menu-btn')`
	menuButtonExpanded = `document.querySelector('.menu-btn').getAttribute('aria-expanded') === 'true'`
	backgroundInert    = `document.getElementById('main').inert && document.querySelector('.skip-link').inert`
	backgroundLive     = `!document.getElementById('main').inert && !document.querySelector('.skip-link').inert`
)

// expectClosed checks everything a closed drawer leaves behind.
func (s *smoke) expectClosed(how string) {
	s.t.Helper()
	s.check(!s.sidebarOpen(), "%s did not close the sidebar", how)
	s.check(!s.eval(menuButtonExpanded), "aria-expanded stayed true after %s", how)
	s.check(s.eval(backgroundLive), "the page behind the drawer stayed inert after %s", how)
	s.check(s.eval(focusOnMenuButton), "focus did not return to the menu button after %s", how)
}

// expectOpen checks a drawer that has finished opening.
func (s *smoke) expectOpen() {
	s.t.Helper()
	if !s.sidebarOpen() {
		s.t.Fatal("the menu button did not open the sidebar: nav.js is not running")
	}
	s.check(s.eval(menuButtonExpanded), "the open drawer did not set aria-expanded on the menu button")
	s.check(s.eval(`document.activeElement === document.querySelector('#sidebar button[data-admin-nav-close]')`), "focus did not move to the drawer's Close button")
	s.check(s.eval(backgroundInert), "the page behind the open drawer, skip link included, is not inert")
	s.check(s.eval(drawerVisibleOnScreen), "the open drawer is not visible on screen")
	s.check(s.eval(`/, 0\.6\)$/.test(getComputedStyle(document.getElementById('sidebar-backdrop')).backgroundColor)`), "the drawer scrim is not drawn at 60 percent")
}

// adminNavToggle drives the phone drawer: open and close it every way the
// script offers, then check the page without the script's mark and the script
// at desktop width. The backdrop is hit-tested first, so the click lands on the
// element the script binds to, not on whatever sits under the point.
func (s *smoke) adminNavToggle() {
	s.t.Helper()
	s.run("admin nav open", chromedp.Click(".menu-btn", chromedp.NodeVisible))
	s.expectOpen()

	ctx, cancel := context.WithTimeout(s.ctx, stepTimeout)
	defer cancel()
	point, err := chromedp.Run(ctx, chromedp.Evaluate[[]float64](
		`[window.innerWidth - 20, window.innerHeight / 2]`))
	if err != nil || len(point) != 2 {
		s.t.Fatalf("viewport point: %v", err)
	}
	hit, err := chromedp.Run(ctx, chromedp.Evaluate[string](
		fmt.Sprintf(`document.elementFromPoint(%g, %g).id`, point[0], point[1])))
	if err != nil {
		s.t.Fatalf("hit test: %v", err)
	}
	if hit != "sidebar-backdrop" {
		s.t.Fatalf("point (%g, %g) is over #%s, not the backdrop", point[0], point[1], hit)
	}
	s.run("admin nav backdrop close", chromedp.MouseClickXY(point[0], point[1]))
	s.expectClosed("a backdrop click")

	s.run("admin nav reopen for Close", chromedp.Click(".menu-btn", chromedp.NodeVisible))
	s.expectOpen()
	s.run("admin nav close button", chromedp.Click("#sidebar button[data-admin-nav-close]", chromedp.NodeVisible))
	s.expectClosed("the Close button")

	s.run("admin nav reopen for Escape", chromedp.Click(".menu-btn", chromedp.NodeVisible))
	s.expectOpen()
	s.run("admin nav escape", chromedp.KeyEvent(kb.Escape))
	s.expectClosed("Escape")

	// Without the script's mark the phone page keeps its model list in the
	// flow and shows no control that needs the script.
	s.check(s.eval(`document.documentElement.classList.contains('nav-js')`), "nav.js did not mark the document")
	s.eval(`document.documentElement.classList.remove('nav-js'), true`)
	s.check(s.eval(drawerVisibleOnScreen), "the model list is hidden on a phone without the script's mark")
	s.check(s.eval(`getComputedStyle(document.getElementById('sidebar')).position !== 'fixed'`), "the sidebar overlays the page on a phone without the script's mark")
	s.check(s.eval(`getComputedStyle(document.querySelector('.menu-btn')).display === 'none'`), "the menu button shows on a phone without the script")
	s.eval(`document.documentElement.classList.add('nav-js'), true`)

	// At desktop width the drawer does not exist: opening is a no-op.
	s.run("admin nav desktop width", chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
		_, err := chromedp.Call(c, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
			Width: 1280, Height: 800, DeviceScaleFactor: 1,
		})
		return err
	}))
	s.eval(`window.tangoAdminNav.open(), true`)
	s.check(!s.sidebarOpen(), "open() opened the drawer at desktop width")
	s.check(s.eval(backgroundLive), "open() left the page inert at desktop width")
	s.run("admin nav phone width", chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
		_, err := chromedp.Call(c, emulation.ClearDeviceMetricsOverride, cdp.Empty{})
		return err
	}))
}

// tabGroups names the changelist region that holds the focused element, or
// "body" when focus has fallen to the page.
const tabGroups = `(function () {
	var el = document.activeElement;
	if (!el || el === document.body) return 'body';
	var regions = [
		['nav', '#sidebar'],
		['search', '#list-search'],
		['filter', 'select[name="filter_pinned"]'],
		['bulk', '[data-bulk-bar]'],
		['row', '.table-admin tbody']
	];
	for (var i = 0; i < regions.length; i++) {
		if (el.closest(regions[i][1])) return regions[i][0];
	}
	return 'other';
})()`

const activeDescription = `document.activeElement ? (document.activeElement.id || document.activeElement.tagName) : ''`

// activeLabel names the focused control by its accessible label when it has
// one, so the Tab walk can tell two links of the same kind apart.
const activeLabel = `(function () { var el = document.activeElement; if (!el || el === document.body) return ''; return el.getAttribute('aria-label') || el.id || el.tagName; })()`

const activeIsSubmit = `!!document.activeElement && document.activeElement.type === 'submit'`

// tabPresses bounds the Tab walk through the changelist. It covers the
// navigation, the toolbar and the first rows.
const tabPresses = 40

// press sends one key to the focused element and fails the run if the browser
// refuses it.
func (s *smoke) press(key string, opts ...chromedp.KeyOption) {
	s.t.Helper()
	if err := s.do(chromedp.KeyEvent(key, opts...)); err != nil {
		s.t.Fatalf("press %q: %v", key, err)
	}
}

// selectFirstRow checks the first row's checkbox by script, so the bulk bar
// appears and the focus starting point stays on the page's first control.
const selectFirstRow = `(document.querySelector('.table-admin tbody input[data-bulk-pk]').click(), true)`

// changelistTabOrder walks Tab through the changelist at desktop width. Focus
// must reach the navigation, the search, the filter, the bulk bar and the row
// controls in that order, and must never fall to the page body.
func (s *smoke) changelistTabOrder() {
	s.t.Helper()
	s.setWidth(1440, 900)
	s.visit("changelist tab order", "/admin/smoke_note/")
	s.check(s.eval(selectFirstRow), "the first row's checkbox could not be selected")

	var sequence, labels []string
	first := map[string]int{}
	for i := 0; i < tabPresses; i++ {
		s.press(kb.Tab)
		group := s.evalString(tabGroups)
		sequence = append(sequence, group)
		labels = append(labels, s.evalString(activeLabel))
		if _, seen := first[group]; !seen {
			first[group] = i
		}
	}
	if _, lost := first["body"]; lost {
		s.t.Errorf("focus fell to the page body during Tab: %v", sequence)
	}
	last := -1
	for _, group := range []string{"nav", "search", "filter", "bulk", "row"} {
		at, reached := first[group]
		if !reached {
			s.t.Errorf("Tab never reached the %s region: %v", group, sequence)
			continue
		}
		s.check(at > last, "the %s region is reached before the region ahead of it: %v", group, sequence)
		last = at
	}
	for _, action := range []string{`.table-actions a:nth-of-type(1)`, `.table-actions a:nth-of-type(2)`} {
		want := s.evalString(`document.querySelector('.table-admin tbody ` + action + `').getAttribute('aria-label')`)
		s.check(slices.Contains(labels, want), "Tab never reached the first row's control %q within %d presses: %v", want, tabPresses, labels)
	}
}

// searchAndFilterKeyboard runs a search and then a filter from the keyboard
// alone. Each result is checked against the page it changes: the search
// narrows the rows to those containing the first row's identity, and the
// filter puts its chosen value in the URL and keeps it selected after the
// reload.
func (s *smoke) searchAndFilterKeyboard() {
	s.t.Helper()
	s.setWidth(1440, 900)
	s.visit("changelist keyboard search", "/admin/smoke_note/")
	identity := s.evalString(`document.querySelector('.table-actions a').getAttribute('aria-label').replace(/^Edit /, '')`)
	expected := s.evalString(`String(Array.from(document.querySelectorAll('.table-admin tbody tr')).filter(function (r) { return r.textContent.indexOf(` + strconv.Quote(identity) + `) >= 0; }).length)`)
	s.run("type a search and press Enter",
		chromedp.Focus("#list-search", chromedp.NodeVisible),
		chromedp.SendKeys("#list-search", identity),
		chromedp.KeyEvent(kb.Enter),
	)
	s.check(s.waitUntil(stepTimeout, `new URLSearchParams(location.search).get('q') === `+strconv.Quote(identity)), "the search did not submit the typed query")
	rows := s.evalString(`String(document.querySelectorAll('.table-admin tbody tr').length)`)
	s.check(rows == expected, "search for %q shows %s rows, want the %s that contain it", identity, rows, expected)

	s.visit("changelist keyboard filter", "/admin/smoke_note/")
	const filter = `select[name="filter_pinned"]`
	options := s.evalString(`Array.from(document.querySelector('` + filter + `').options).map(function (o) { return o.value; }).join('|')`)
	s.check(len(strings.Split(options, "|")) >= 2, "the pinned filter has no value to choose: %q", options)
	chosen := strings.Split(options, "|")[1]
	s.check(chosen != "", "the first filter value after Any is empty: %q", options)
	s.run("focus the search and Tab to the filter", chromedp.Focus("#list-search", chromedp.NodeVisible))
	s.press(kb.Tab)
	s.check(s.eval(`!!document.activeElement && document.activeElement.matches('`+filter+`')`), "Tab from the search did not reach the filter: focus is %q", s.evalString(describeActive))
	s.press(chosen[:1])
	s.check(s.evalString(`document.querySelector('`+filter+`').value`) == chosen, "the keyboard did not change the filter to %q: value is %q", chosen, s.evalString(`document.querySelector('`+filter+`').value`))
	for i := 0; i < 10 && !s.eval(activeIsSubmit); i++ {
		s.press(kb.Tab)
	}
	s.run("submit the filter with the keyboard", chromedp.KeyEvent(kb.Enter))
	s.check(s.waitUntil(stepTimeout, `new URLSearchParams(location.search).get('filter_pinned') === `+strconv.Quote(chosen)), "the filter did not apply %q", chosen)
	s.check(s.evalString(`document.querySelector('`+filter+`').value`) == chosen, "the filter is not %q after the reload", chosen)
}

// bulkDialogKeyboard opens the bulk dialog from its trigger with Enter, then
// closes it with Escape. Focus must enter the dialog, and the close must
// return focus to the trigger. Nothing is deleted.
func (s *smoke) bulkDialogKeyboard() {
	s.t.Helper()
	s.setWidth(1440, 900)
	s.visit("bulk dialog keyboard", "/admin/smoke_note/")
	s.check(s.eval(selectFirstRow), "the first row's checkbox could not be selected")
	s.check(s.eval(`(document.querySelector('[data-bulk-open]').focus(), true)`), "the bulk trigger could not take focus")
	s.run("bulk open from keyboard", chromedp.KeyEvent(kb.Enter))
	const dialog = `document.querySelector('[data-bulk-dialog]')`
	s.check(s.eval(dialog+`.open`), "Enter on the bulk trigger did not open the dialog")
	s.check(s.eval(dialog+`.contains(document.activeElement)`), "focus is not inside the bulk dialog after it opened; focus is on %s", s.evalString(activeDescription))
	s.run("bulk close with Escape", chromedp.KeyEvent(kb.Escape))
	s.check(!s.eval(dialog+`.open`), "Escape did not close the bulk dialog")
	s.check(s.eval(`document.activeElement === document.querySelector('[data-bulk-open]')`), "focus did not return to the bulk trigger after Escape; focus is on %s", s.evalString(activeDescription))
}

// phoneDrawerKeyboard opens the phone drawer from the menu button with Enter
// and walks Tab around it. Focus must stay inside the drawer, Shift+Tab from
// its first control must wrap to its last, and Escape must close it and return
// focus to the menu button.
func (s *smoke) phoneDrawerKeyboard() {
	s.t.Helper()
	s.setWidth(mobileWidth, mobileHeight)
	s.visit("phone drawer keyboard", "/admin/smoke_note/")
	s.check(s.eval(`(document.querySelector('.menu-btn').focus(), true)`), "the menu button could not take focus")
	s.run("drawer open from keyboard", chromedp.KeyEvent(kb.Enter))
	s.expectOpen()

	const controls = `Array.prototype.slice.call(document.getElementById('sidebar').querySelectorAll('a[href], button:not([disabled]), input:not([disabled])'))`
	count := atoi(s.evalString(`String(` + controls + `.length)`))
	for i := 0; i < count+2; i++ {
		s.press(kb.Tab)
		s.check(s.eval(`document.getElementById('sidebar').contains(document.activeElement)`), "Tab press %d left the drawer; focus is on %s", i+1, s.evalString(activeDescription))
	}

	s.check(s.eval(`(`+controls+`[0].focus(), true)`), "the drawer has no first control to focus")
	s.press(kb.Tab, chromedp.KeyModifiers(chromedp.ModifierShift))
	s.check(s.eval(`(function () { var c = `+controls+`; return document.activeElement === c[c.length - 1]; })()`), "Shift+Tab from the first drawer control did not wrap to the last; focus is on %s", s.evalString(activeDescription))

	s.run("drawer close with Escape", chromedp.KeyEvent(kb.Escape))
	s.expectClosed("Escape after Tab")
}

// phoneChangelist checks the changelist at the phone width: the page does not
// scroll sideways, the table does not scroll inside its wrapper, and every
// row's Edit and Delete link sits inside the viewport.
func (s *smoke) phoneChangelist() {
	s.t.Helper()
	s.check(s.eval(`document.documentElement.scrollWidth <= window.innerWidth`), "the changelist scrolls sideways at the phone width")
	s.check(s.eval(`(function () { var w = document.querySelector('.table-wrap'); return !w || w.scrollWidth <= w.clientWidth; })()`), "the changelist table scrolls inside its wrapper at the phone width")
	s.check(s.eval(rowActionsInViewport), "a row's Edit or Delete link is outside the phone viewport")
	s.check(s.eval(phoneControlsReachable), "the search, the filter or a page link is outside the phone viewport, or a page link is under 24px")
}

// phoneControlsReachable checks that the search field, the filter and the
// pagination links all exist and sit inside the phone viewport, and that each
// page link is at least 24px square.
const phoneControlsReachable = `(function () {
	var required = ['#list-search', 'select[name="filter_pinned"]', 'nav[aria-label="Pagination"] a.page-link'];
	if (!required.every(function (sel) { return document.querySelector(sel) !== null; })) return false;
	var controls = document.querySelectorAll(required.join(', '));
	return Array.prototype.every.call(controls, function (el) {
		var r = el.getBoundingClientRect();
		var inside = r.width > 0 && r.left >= 0 && r.right <= window.innerWidth;
		if (el.matches('a.page-link')) return inside && r.width >= 24 && r.height >= 24;
		return inside;
	});
})()`

const rowActionsInViewport = `(function () {
	var links = document.querySelectorAll('.table-admin tbody a.btn-ghost');
	if (links.length < 2) return false;
	return Array.prototype.every.call(links, function (a) {
		var r = a.getBoundingClientRect();
		return r.width > 0 && r.left >= 0 && r.right <= window.innerWidth;
	});
})()`

// bulkConfirmFollowsCount selects one changelist row and then a second, opens
// the bulk dialog each time, and checks that its confirm button reads the
// singular message for one row and the plural message for two. Each dialog is
// cancelled, so no row is deleted.
func (s *smoke) bulkConfirmFollowsCount() {
	s.t.Helper()
	s.run("bulk select first row", chromedp.Click(".table-admin tbody tr:nth-child(1) input[data-bulk-pk]", chromedp.NodeVisible))
	s.run("bulk open with one row", chromedp.Click("[data-bulk-open]", chromedp.NodeVisible))
	one := s.evalString(confirmText)
	oneWant := s.evalString(`document.querySelector('[data-bulk-delete]').getAttribute('data-msg-confirm-one') || ''`)
	s.run("bulk cancel with one row", chromedp.Click("[data-bulk-cancel]", chromedp.NodeVisible))

	s.run("bulk select second row", chromedp.Click(".table-admin tbody tr:nth-child(2) input[data-bulk-pk]", chromedp.NodeVisible))
	s.run("bulk open with two rows", chromedp.Click("[data-bulk-open]", chromedp.NodeVisible))
	many := s.evalString(confirmText)
	manyWant := strings.ReplaceAll(s.evalString(`document.querySelector('[data-bulk-delete]').getAttribute('data-msg-confirm-many') || ''`), "{count}", "2")
	s.run("bulk cancel with two rows", chromedp.Click("[data-bulk-cancel]", chromedp.NodeVisible))

	if oneWant == "" || manyWant == "" {
		s.t.Fatal("the bulk dialog carries no confirm message: bulk.js or its data-msg attributes are missing")
	}
	s.check(one == oneWant, "the bulk confirm button read %q with one row selected, want %q", one, oneWant)
	s.check(many == manyWant, "the bulk confirm button read %q with two rows selected, want %q", many, manyWant)
}

// bulkBarFollowsSelection checks the bar in both states with the script
// running: with no row picked the count slot shows the visible reason, Delete
// selected is disabled and described by that reason; with one row picked the
// count shows the selection, Delete selected is enabled and not described.
// Select-all turns indeterminate with one of several rows picked, and the
// dialog takes focus on Cancel, closes on Escape and returns focus to the
// Delete selected button. The dialog is closed without a submit.
func (s *smoke) bulkBarFollowsSelection() {
	s.t.Helper()
	const bar = `document.querySelector('[data-bulk-bar]').hidden`
	const count = `document.querySelector('[data-bulk-count]').textContent`
	const open = `document.querySelector('[data-bulk-open]')`
	const meta = `document.querySelector('[data-bulk-delete]')`
	none := s.evalString(meta + `.getAttribute('data-msg-none') || ''`)
	s.check(s.evalString("String("+bar+")") == "false", "the bulk bar is hidden with the script running")
	s.check(s.evalString(count) == none, "with no row picked the count slot reads %q, want the reason %q", s.evalString(count), none)
	s.check(s.evalString("String("+open+".disabled)") == "true", "Delete selected is enabled with no row picked")
	s.check(s.evalString("String("+open+".getAttribute('aria-describedby') === 'bulk-count')") == "true", "Delete selected is not described by the reason with no row picked")

	s.run("bulk bar select first row", chromedp.Click(".table-admin tbody tr:nth-child(1) input[data-bulk-pk]", chromedp.NodeVisible))
	one := strings.ReplaceAll(s.evalString(meta+`.getAttribute('data-msg-selected') || ''`), "{count}", "1")
	s.check(s.evalString(count) == one, "with one row picked the count slot reads %q, want %q", s.evalString(count), one)
	s.check(s.evalString("String("+open+".disabled)") == "false", "Delete selected is disabled with one row picked")
	s.check(s.evalString("String("+open+".hasAttribute('aria-describedby'))") == "false", "Delete selected is still described by the reason with one row picked")
	s.check(s.evalString(`String(document.querySelector('[data-bulk-select-all]').indeterminate)`) == "true", "select-all is not indeterminate with one of several rows picked")

	s.run("bulk bar open dialog", chromedp.Click("[data-bulk-open]", chromedp.NodeVisible))
	wantTitle := s.evalString(meta + `.getAttribute('data-msg-title-one') || ''`)
	s.check(s.evalString(`document.querySelector('[data-bulk-title]').textContent`) == wantTitle, "the dialog title is not the singular count copy %q", wantTitle)
	s.check(s.evalString(`String(document.activeElement && document.activeElement.hasAttribute('data-bulk-cancel'))`) == "true", "the dialog did not open with Cancel focused")
	s.run("bulk bar escape closes", chromedp.KeyEvent(kb.Escape))
	s.check(s.evalString(`String(document.querySelector('[data-bulk-dialog]').open)`) == "false", "Escape did not close the dialog")
	s.check(s.evalString(`String(document.activeElement && document.activeElement.hasAttribute('data-bulk-open'))`) == "true", "focus did not return to Delete selected")
}

// bulkListCapAndPhoneSelectAll selects every row on the page at the desktop
// width and checks that the dialog lists at most the cap and then names the
// rest. At the phone width it checks that the visible select-all control picks
// every row and is at least 24px square.
func (s *smoke) bulkListCapAndPhoneSelectAll() {
	s.t.Helper()
	const meta = `document.querySelector('[data-bulk-delete]')`
	const open = `document.querySelector('[data-bulk-open]')`
	startWidth := s.evalString(`String(window.innerWidth)`)
	startHeight := s.evalString(`String(window.innerHeight)`)

	s.setWidth(1280, 800)
	s.run("bulk select all at desktop width", chromedp.Click(".table-admin thead input[data-bulk-select-all]", chromedp.NodeVisible))
	rows := s.evalString(`String(document.querySelectorAll('[data-bulk-pk]').length)`)
	s.run("bulk open with every row", chromedp.Click("[data-bulk-open]", chromedp.NodeVisible))
	limit := s.evalString(meta + `.getAttribute('data-bulk-limit') || ''`)
	listed := s.evalString(`String(document.querySelectorAll('[data-bulk-list] li').length)`)
	s.check(listed == "11", "the dialog lists %s items for %s rows, want the cap of %s and one more line", listed, rows, limit)
	more := strings.ReplaceAll(s.evalString(meta+`.getAttribute('data-msg-more-many') || ''`), "{count}", fmt.Sprint(atoi(rows)-atoi(limit)))
	s.check(s.evalString(`document.querySelector('[data-bulk-list] li:last-child').textContent`) == more, "the last line of the dialog is not %q", more)
	wantTitleMany := strings.ReplaceAll(s.evalString(meta+`.getAttribute('data-msg-title-many') || ''`), "{count}", rows)
	s.check(s.evalString(`document.querySelector('[data-bulk-title]').textContent`) == wantTitleMany, "the dialog title is not the plural count copy %q", wantTitleMany)
	s.run("bulk cancel after cap", chromedp.Click("[data-bulk-cancel]", chromedp.NodeVisible))

	s.run("bulk clear every row", chromedp.Click(".table-admin thead input[data-bulk-select-all]", chromedp.NodeVisible))
	for i := 1; i <= atoi(limit)+1; i++ {
		s.run(fmt.Sprintf("bulk pick row %d", i), chromedp.Click(fmt.Sprintf(".table-admin tbody tr:nth-child(%d) input[data-bulk-pk]", i), chromedp.NodeVisible))
	}
	s.run("bulk open one over the cap", chromedp.Click("[data-bulk-open]", chromedp.NodeVisible))
	s.check(s.evalString(`String(document.querySelectorAll('[data-bulk-list] li').length)`) == "11", "the dialog with one row past the cap did not list the cap and one more line")
	oneMore := s.evalString(meta + `.getAttribute('data-msg-more-one') || ''`)
	s.check(s.evalString(`document.querySelector('[data-bulk-list] li:last-child').textContent`) == oneMore, "the last line for one row past the cap is not %q", oneMore)
	titleOver := strings.ReplaceAll(s.evalString(meta+`.getAttribute('data-msg-title-many') || ''`), "{count}", fmt.Sprint(atoi(limit)+1))
	s.check(s.evalString(`document.querySelector('[data-bulk-title]').textContent`) == titleOver, "the dialog title for one row past the cap is not %q", titleOver)
	s.run("bulk cancel one over the cap", chromedp.Click("[data-bulk-cancel]", chromedp.NodeVisible))
	s.run("bulk select the page again", chromedp.Click(".table-admin thead input[data-bulk-select-all]", chromedp.NodeVisible))
	s.check(s.evalString(`String(document.querySelectorAll('[data-bulk-pk]:not(:checked)').length)`) == "0", "the desktop select-all did not pick every row after the one-over-cap case")

	s.setWidth(390, 844)
	const phoneAll = "[data-bulk-bar] label input[data-bulk-select-all]"
	s.run("bulk phone select all clears the page", chromedp.Click(phoneAll, chromedp.NodeVisible))
	s.check(s.evalString(`String(document.querySelectorAll('[data-bulk-pk]:checked').length)`) == "0", "the phone select-all control did not clear every row from a full selection")
	none := s.evalString(meta + `.getAttribute('data-msg-none') || ''`)
	s.check(s.evalString(`document.querySelector('[data-bulk-count]').textContent`) == none, "after clearing every row the count slot did not return to the reason %q", none)
	s.check(s.evalString("String("+open+".disabled)") == "true", "Delete selected is enabled after clearing every row")
	s.run("bulk phone select all picks the page", chromedp.Click(phoneAll, chromedp.NodeVisible))
	s.check(s.evalString(`String(document.querySelectorAll('[data-bulk-pk]:not(:checked)').length)`) == "0", "the phone select-all control did not pick every row")
	s.check(s.evalString(`String(document.querySelector('[data-bulk-open]').disabled)`) == "false", "Delete selected is disabled after the phone select-all")
	s.check(s.evalString(`(function () { var r = document.querySelector('[data-bulk-bar] label').getBoundingClientRect(); return String(r.width >= 24 && r.height >= 24 && r.right <= window.innerWidth); })()`) == "true", "the phone select-all control is under 24px or outside the viewport")
	s.check(s.evalString(`String(document.querySelector('[data-bulk-bar] label').offsetParent !== null)`) == "true", "the phone select-all control is not visible")
	s.setWidth(atoi(startWidth), atoi(startHeight))
	s.check(s.eval(`!!`+open), "the Delete selected button is missing after the width change")
}

// atoi reads a whole number the page or the browser reported.
func atoi(value string) int {
	var n int
	fmt.Sscan(value, &n)
	return n
}

// startBrowser starts a headless browser at the phone viewport. The returned
// cancel ends the session and the browser process.
func startBrowser(t *testing.T, binary string) (context.Context, func()) {
	t.Helper()
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(binary), chromedp.WindowSize(mobileWidth, mobileHeight))...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocator)
	return browserCtx, func() {
		cancelBrowser()
		cancelAllocator()
	}
}

// adminLogin signs in as the smoke admin and waits for the admin sidebar.
func (s *smoke) adminLogin() {
	s.t.Helper()
	s.visit("admin login", "/admin/login/")
	s.run("admin login submit",
		chromedp.SendKeys("#field-username", smokeAdminUser),
		chromedp.SendKeys("#field-password", smokeAdminPassword),
		chromedp.Submit("#field-password"),
		chromedp.WaitVisible("#sidebar"),
	)
}

// setWidth sets the emulated viewport size.
func (s *smoke) setWidth(width, height int) {
	s.t.Helper()
	s.run(fmt.Sprintf("viewport %dx%d", width, height), chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
		_, err := chromedp.Call(c, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
			Width: int64(width), Height: int64(height), DeviceScaleFactor: 1,
		})
		return err
	}))
}

const confirmText = `document.querySelector('[data-bulk-confirm]').textContent`

// evalString evaluates expr and returns its string value.
func (s *smoke) evalString(expr string) string {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(s.ctx, stepTimeout)
	defer cancel()
	value, err := chromedp.Run(ctx, chromedp.Evaluate[string](expr))
	if err != nil {
		s.t.Fatalf("evaluate %q: %v", expr, err)
	}
	return value
}

// previewHeading reads the first heading of the preview frame from the DOM.
// The frame is sandboxed without allow-scripts, so the test reads it through
// the DevTools DOM domain rather than from script.
func (s *smoke) previewHeading() (string, error) {
	ctx, cancel := context.WithTimeout(s.ctx, stepTimeout)
	defer cancel()
	depth, pierce := int64(-1), true
	result, err := chromedp.Call(ctx, dom.GetDocument, dom.GetDocumentParams{Depth: &depth, Pierce: &pierce})
	if err != nil {
		return "", err
	}
	frame := findNode(result.Root, func(n *cdp.Node) bool { return attribute(n, "id") == "field-Body-preview" })
	if frame == nil {
		return "", errors.New("the preview frame is not in the DOM tree")
	}
	if frame.ContentDocument == nil {
		// A sandboxed frame with an opaque origin can run in its own process,
		// and then the parent's DOM tree does not include its document. Its
		// target is attached on its own instead.
		return s.previewHeadingInOwnTarget(ctx)
	}
	heading := findNode(frame.ContentDocument, func(n *cdp.Node) bool { return n.NodeName == "H1" })
	if heading == nil {
		return "", errors.New("the preview frame has no heading")
	}
	return textOf(heading), nil
}

// previewHeadingInOwnTarget reads the heading from the iframe target that
// holds the preview's document.
func (s *smoke) previewHeadingInOwnTarget(ctx context.Context) (string, error) {
	targets, err := chromedp.Targets(ctx)
	if err != nil {
		return "", err
	}
	var frameID string
	var seen []string
	for _, info := range targets {
		seen = append(seen, info.Type+" "+info.URL)
		if info.Type == "iframe" && info.URL == "about:srcdoc" {
			frameID = string(info.TargetID)
		}
	}
	if frameID == "" {
		return "", fmt.Errorf("no srcdoc iframe target among %q", seen)
	}
	// The attached context is kept until the test ends. Cancelling it during
	// the run stalled the session, and the next navigation never completed.
	if s.frameCtx == nil {
		frameCtx, cancelFrame := chromedp.NewContext(s.ctx, chromedp.WithTargetID(target.ID(frameID)))
		s.t.Cleanup(cancelFrame)
		s.frameCtx = frameCtx
	}
	heading, err := chromedp.Run(s.frameCtx, chromedp.Text("h1"))
	if err != nil {
		return "", fmt.Errorf("heading in the preview target: %w", err)
	}
	return heading, nil
}

// textOf concatenates the text children of a node.
func textOf(n *cdp.Node) string {
	var text strings.Builder
	for _, child := range n.Children {
		if child.NodeType == 3 {
			text.WriteString(child.NodeValue)
		}
	}
	return text.String()
}

// findNode returns the first node under n, in document order, that matches,
// descending into iframe content documents.
func findNode(n *cdp.Node, match func(*cdp.Node) bool) *cdp.Node {
	if n == nil {
		return nil
	}
	if match(n) {
		return n
	}
	if found := findNode(n.ContentDocument, match); found != nil {
		return found
	}
	for _, child := range n.Children {
		if found := findNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func attribute(n *cdp.Node, name string) string {
	for i := 0; i+1 < len(n.Attributes); i += 2 {
		if n.Attributes[i] == name {
			return n.Attributes[i+1]
		}
	}
	return ""
}

// adminMarkdownPreview switches the Markdown field to its preview tab and
// waits for the sandboxed frame to show the rendered heading.
func (s *smoke) adminMarkdownPreview() {
	s.t.Helper()
	s.run("admin markdown preview",
		chromedp.SetValue("#field-Body", "# Hello", chromedp.NodeVisible),
		chromedp.Click("#field-Body-tab-preview", chromedp.NodeVisible),
	)
	deadline := time.Now().Add(10 * time.Second)
	last := ""
	for {
		heading, err := s.previewHeading()
		if err == nil && heading == "# Hello" {
			return
		}
		if err != nil {
			last = err.Error()
		} else {
			last = fmt.Sprintf("heading %q", heading)
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("the sandboxed preview frame did not render the heading: %s", last)
		}
		s.settle()
	}
}

// inactivePanelsHidden checks that the script hid the panel of each widget's
// tab that is not selected, so only the Edit and Write panels show.
func (s *smoke) inactivePanelsHidden() {
	s.t.Helper()
	s.check(s.eval(`document.getElementById('field-Payload-panel-view').hidden === true`), "the JSON Formatted panel is visible before any tab is chosen")
	s.check(s.eval(`document.getElementById('field-Body-panel-preview').hidden === true`), "the Markdown Preview panel is visible before any tab is chosen")
	s.check(s.eval(`document.getElementById('field-Payload-panel-edit').hidden === false`), "the JSON Edit panel is hidden")
}

// adminWidgetTabs types an invalid document into the JSON field with the
// keyboard, opens its Formatted tab with Enter, and checks the one message the
// status line shows. The message must be the plain sentence and must not
// repeat any part of the document: the browser's own parse message quotes it.
func (s *smoke) adminWidgetTabs() {
	s.t.Helper()
	const document = `{"secret": oops}`
	s.check(s.eval(`(document.getElementById('field-Payload').focus(), true)`), "the JSON field is not in the page")
	s.run("admin json type an invalid document", chromedp.KeyEvent(document))
	s.check(s.evalString(`document.getElementById('field-Payload').value`) == document, "typed text did not reach the JSON field")
	s.check(s.eval(`(document.getElementById('field-Payload-tab-view').focus(), true)`), "the Formatted tab is not in the page")
	s.run("admin json open the Formatted tab with Enter", chromedp.KeyEvent(kb.Enter))

	const status = `document.querySelector('[data-json-status]').textContent`
	const sentence = "This is not valid JSON. Check that commas, quotes and brackets are in place."
	s.check(s.evalString(status) == sentence, "the status line shows %q, want %q", s.evalString(status), sentence)
	s.check(s.eval(`document.querySelector('[data-json-status]').classList.contains('field-error')`), "the invalid status is not in the field error style")
	s.check(s.evalString(`document.querySelector('[data-json-formatted]').textContent`) == "", "the Formatted box holds text for an invalid document")
	for _, leaked := range []string{"secret", "oops"} {
		s.check(!strings.Contains(s.evalString(status), leaked), "the status line quotes %q", leaked)
	}
	s.tabKeyboard("field-Payload-tab-edit", "field-Payload-tab-view")
	s.tabKeyboard("field-Body-tab-write", "field-Body-tab-preview")
}

// tabKeyboard checks the tab keyboard model on one tab list, starting from its
// first tab: End moves to the last tab, Home back to the first, and the arrow
// keys step between them, wrapping at either end.
func (s *smoke) tabKeyboard(first, last string) {
	s.t.Helper()
	s.check(s.eval(`(document.getElementById('`+first+`').focus(), true)`), "tab %q is not in the page", first)
	s.keyMove(kb.ArrowLeft, last)
	s.keyMove(kb.ArrowRight, first)
	s.keyMove(kb.End, last)
	s.keyMove(kb.Home, first)
	s.keyMove(kb.ArrowRight, last)
	s.keyMove(kb.ArrowLeft, first)
}

// keyMove presses key on the focused tab and checks where focus lands: on the
// want tab, which is the only selected tab, with its panel shown.
func (s *smoke) keyMove(key, want string) {
	s.t.Helper()
	s.run("press a key on a tab", chromedp.KeyEvent(key))
	const active = `document.activeElement ? document.activeElement.id : ''`
	s.check(s.evalString(active) == want, "after one key focus is on %q, want %q", s.evalString(active), want)
	selected := s.evalString(`document.getElementById('` + want + `').getAttribute('aria-selected')`)
	s.check(selected == "true", "tab %q is aria-selected %q after the key, want true", want, selected)
	s.check(s.eval(`!document.getElementById(document.getElementById('`+want+`').getAttribute('aria-controls')).hidden`), "the panel of tab %q is hidden after the key", want)
}

// adminFormSummary saves a create form with two problems and checks that focus
// moves to the summary heading, then that its link moves focus to the field.
func (s *smoke) adminFormSummary() {
	s.t.Helper()
	s.run("admin create form with two problems",
		chromedp.SetValue("#field-Code", "toolong", chromedp.NodeVisible),
		chromedp.SetValue("#field-Label", "toolong"),
		chromedp.Click("form[method=post] button.btn-primary", chromedp.NodeVisible),
		chromedp.WaitVisible("#form-summary"),
	)
	const active = `document.activeElement ? document.activeElement.id : ''`
	s.check(s.evalString(active) == "form-summary", "focus after a failed save is on %q, want the summary heading", s.evalString(active))
	s.run("summary link to the first field", chromedp.Click(`.alert-error a[href="#field-Code"]`, chromedp.NodeVisible))
	s.check(s.evalString(active) == "field-Code", "focus after the summary link is on %q, want the field", s.evalString(active))

	// One problem has no summary, so its control takes focus.
	s.run("admin create form with one problem",
		chromedp.Navigate(s.base+"/admin/smoke_code/new/"),
		chromedp.WaitVisible("#field-Code"),
		chromedp.SetValue("#field-Code", "toolong", chromedp.NodeVisible),
		chromedp.Click("form[method=post] button.btn-primary", chromedp.NodeVisible),
		chromedp.WaitVisible("#field-Code[aria-invalid]"),
	)
	s.check(!s.eval(`!!document.getElementById('form-summary')`), "a single field problem should have no summary")
	s.check(s.evalString(active) == "field-Code", "focus after one field problem is on %q, want the invalid control", s.evalString(active))
}

// describeActive names the focused control by id, or save for the submit
// button, and is empty when focus is on the page body.
const describeActive = `(function (e) { if (!e || e === document.body) return ''; return e.id || (e.type === 'submit' ? 'save' : e.tagName); })(document.activeElement)`

// createOrder lists the create form's keyboard path in DOM order, ending at the
// first submit button.
const createOrder = `(function () {
	var out = [];
	var els = document.getElementById('field-Code').form.querySelectorAll('input:not([type=hidden]),select,textarea,button,a[href]');
	for (var i = 0; i < els.length; i++) {
		var e = els[i];
		if (e.disabled || e.getClientRects().length === 0) continue;
		out.push(e.id || (e.type === 'submit' ? 'save' : e.tagName));
		if (e.type === 'submit') break;
	}
	return out.join('|');
})()`

// focusRingShown reports whether the focused control is focus-visible and
// carries the 4px ring that the focus style paints.
const focusRingShown = `(function () { var e = document.activeElement; return !!e && e.matches(':focus-visible') && /0px 0px 0px 4px/.test(getComputedStyle(e).boxShadow); })()`

// createFormKeyboard walks Tab through the create form to Save in DOM order,
// with a visible focus ring on each control, types a valid record and saves it
// with Enter. The save returns to the changelist with a status notice, which is
// then dismissed from the keyboard.
func (s *smoke) createFormKeyboard() {
	s.t.Helper()
	s.visit("admin create form keyboard", "/admin/smoke_code/new/")
	order := strings.Split(s.evalString(createOrder), "|")
	s.check(len(order) >= 3 && order[len(order)-1] == "save", "the create form's keyboard path does not end at Save: %v", order)
	for i := 0; i < dismissPresses && s.evalString(describeActive) != "field-Code"; i++ {
		s.press(kb.Tab)
	}
	s.check(s.evalString(describeActive) == "field-Code", "Tab never reached the first field of the create form within %d presses", dismissPresses)
	for i, want := range order {
		got := s.evalString(describeActive)
		s.check(got == want, "Tab step %d reached %q, want %q in DOM order: %v", i, got, want, order)
		s.check(s.eval(focusRingShown), "the focused control %q shows no focus ring: box-shadow %q", want, s.evalString(`getComputedStyle(document.activeElement).boxShadow`))
		if want == "field-Code" || want == "field-Label" {
			s.run("type a valid value into "+want, chromedp.SendKeys("#"+want, "ok"))
		}
		if i < len(order)-1 {
			s.press(kb.Tab)
		}
	}

	s.run("save the create form with Enter", chromedp.KeyEvent(kb.Enter))
	s.check(s.waitUntil(stepTimeout, `location.pathname === '/admin/smoke_code/'`), "Enter on Save did not return to the changelist")
	s.check(s.waitUntil(stepTimeout, `!!document.querySelector('#admin-status .status-dismiss button')`), "the saved record shows no status notice with a Dismiss control")

	for i := 0; i < dismissPresses; i++ {
		if s.eval(`!!document.activeElement && !!document.activeElement.form && document.activeElement.form.classList.contains('status-dismiss')`) {
			break
		}
		s.press(kb.Tab)
	}
	s.check(s.eval(`!!document.activeElement && !!document.activeElement.form && document.activeElement.form.classList.contains('status-dismiss')`), "Tab never reached the status notice's Dismiss control within %d presses", dismissPresses)
	s.run("dismiss the notice with Enter", chromedp.KeyEvent(kb.Enter))
	s.check(s.waitUntil(stepTimeout, `!document.querySelector('#admin-status .status-dismiss')`), "Enter on Dismiss left the notice on the page")
	s.check(s.evalString(`document.activeElement ? document.activeElement.id : ''`) == "main", "after the notice was dismissed focus is on %q, want the content region", s.evalString(describeActive))
}

// dismissPresses bounds the Tab walk from the top of the changelist to the
// status notice's Dismiss control.
const dismissPresses = 80

// headerOf returns the Content-Security-Policy the server sends for path.
func headerOf(t *testing.T, server *httptest.Server, path string) string {
	t.Helper()
	response, err := http.Get(server.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer response.Body.Close()
	return response.Header.Get("Content-Security-Policy")
}

func TestHeadlessBrowserReadsNoUnexpectedCSPViolations(t *testing.T) {
	binary := chromeBinary(t)
	if binary == "" {
		if os.Getenv("TANGO_REQUIRE_BROWSER") == "1" {
			t.Fatal("TANGO_REQUIRE_BROWSER=1 but no Chrome or Chromium was found; install one or set TANGO_BROWSER_PATH")
		}
		t.Skip("no Chrome or Chromium found locally; the headless CSP smoke did not run (set TANGO_BROWSER_PATH, or TANGO_REQUIRE_BROWSER=1 to fail instead)")
	}

	server := httptest.NewServer(composedSite(t))

	// The headers are checked over HTTP as well, so a page the browser loads
	// under the wrong policy fails as a header mismatch, not only as a
	// violation.
	for path, want := range map[string]string{
		"/accounts/login/": appHeader,
		"/admin/login/":    adminHeader,
		"/docs/":           docsHeader,
		"/openapi.json":    appHeader,
	} {
		if got := headerOf(t, server, path); got != want {
			t.Errorf("%s Content-Security-Policy = %q, want %q", path, got, want)
		}
	}

	browserCtx, cancelBrowser := startBrowser(t, binary)

	rec := &recorder{origin: server.URL}
	// The subscriptions start the browser and must exist before the first
	// navigation, so no violation of the first page is missed. The cleanup
	// cancels the session before waiting for the readers, because the
	// subscriptions end only when their context does.
	var readers sync.WaitGroup
	consume(&readers, rec, chromedp.Events(browserCtx, runtime.ConsoleAPICalled), rec.console)
	consume(&readers, rec, chromedp.Events(browserCtx, log.EntryAdded), func(ev log.EventEntryAdded) {
		if ev.Entry.Source == log.EntrySourceSecurity {
			rec.add("browser log", ev.Entry.Text)
		}
	})
	consume(&readers, rec, chromedp.Events(browserCtx, network.RequestWillBeSent), func(ev network.EventRequestWillBeSent) {
		if !rec.sameOrigin(ev.Request.URL) {
			rec.add("request", ev.Request.Method+" "+ev.Request.URL)
		}
	})
	t.Cleanup(func() {
		cancelBrowser()
		readers.Wait()
		server.Close()
	})

	rec.begin("setup")
	if _, err := chromedp.Call(browserCtx, page.AddScriptToEvaluateOnNewDocument,
		page.AddScriptToEvaluateOnNewDocumentParams{Source: reporterScript}); err != nil {
		t.Fatalf("install the reporter script: %v", err)
	}

	s := &smoke{t: t, ctx: browserCtx, base: server.URL, rec: rec}
	s.positiveControl()

	s.visit("accounts login", "/accounts/login/")
	s.visit("accounts register", "/accounts/register/")
	s.visit("accounts password reset", "/accounts/password-reset/")

	s.adminLogin()
	s.visit("admin dashboard", "/admin/")
	s.visit("admin changelist", "/admin/smoke_note/")
	s.phoneChangelist()
	s.bulkConfirmFollowsCount()
	s.visit("admin changelist bulk bar", "/admin/smoke_note/")
	s.bulkBarFollowsSelection()
	s.bulkListCapAndPhoneSelectAll()
	s.adminNavToggle()
	s.changelistTabOrder()
	s.searchAndFilterKeyboard()
	s.bulkDialogKeyboard()
	s.phoneDrawerKeyboard()
	s.visit("admin change form", "/admin/smoke_note/1/")
	s.inactivePanelsHidden()
	s.adminMarkdownPreview()
	s.adminWidgetTabs()
	s.visit("admin create form", "/admin/smoke_code/new/")
	s.adminFormSummary()
	s.createFormKeyboard()

	s.visit("swagger ui", "/docs/")
	rendered := s.waitUntil(swaggerTimeout, `!!document.querySelector('.swagger-ui')`)
	s.report()
	if !rendered {
		s.t.Fatal("the Swagger UI page did not render its UI within the timeout")
	}
}

// swaggerTimeout bounds the wait for the Swagger UI bundle to build its page.
const swaggerTimeout = 30 * time.Second

// waitUntil polls expr until it is true or the timeout passes, and reports
// whether it became true.
func (s *smoke) waitUntil(timeout time.Duration, expr string) bool {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for !s.eval(expr) {
		if time.Now().After(deadline) {
			return false
		}
		s.settle()
	}
	return true
}

// report fails on every finding the browser reported, in the order it reported
// them.
func (s *smoke) report() {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	for _, f := range s.rec.findings {
		s.t.Errorf("%s: %s: %s", f.page, f.source, f.detail)
	}
}
