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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

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
	ID    int64 `tango:"pk"`
	Title string
	Body  string
}

// previewNote is the host's Markdown renderer for the smoke. It emits a
// heading, so the sandboxed frame has an element the test can read.
func previewNote(_ context.Context, source string) (template.HTML, error) {
	return template.HTML("<h1>" + template.HTMLEscapeString(source) + "</h1>"), nil
}

func notesApp() tango.App {
	return tango.NewApp("notes", func(registry *tango.Registry) error {
		if err := registry.Models().Register(smokeNote{}); err != nil {
			return err
		}
		return registry.Admin().Register(smokeNote{}, admin.Options{
			ListDisplay: []string{"Title"},
			Search:      []string{"Title"},
			BulkDelete:  true,
			Widgets: map[string]admin.Widget{
				"Body": admin.Markdown(admin.WithMarkdownPreview(previewNote)),
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
		smokeNote{},
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
	if _, err := sqlDB.Exec(`INSERT INTO smoke_note (id, title, body) VALUES (1, 'Hello', 'Body')`); err != nil {
		t.Fatalf("seed note: %v", err)
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

// adminNavToggle opens the sidebar with the menu button, then closes it by
// clicking the backdrop. The backdrop is hit-tested first, so the click lands
// on the element the script binds to, not on whatever sits under the point.
func (s *smoke) adminNavToggle() {
	s.t.Helper()
	s.run("admin nav open", chromedp.Click(".menu-btn", chromedp.NodeVisible))
	if !s.sidebarOpen() {
		s.t.Fatal("the menu button did not open the sidebar: nav.js is not running")
	}

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
	s.run("admin nav close", chromedp.MouseClickXY(point[0], point[1]))
	if s.sidebarOpen() {
		s.t.Fatal("the backdrop did not close the sidebar: nav.js is not running")
	}
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

	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(binary), chromedp.WindowSize(mobileWidth, mobileHeight))...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocator)

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
		cancelAllocator()
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

	s.visit("admin login", "/admin/login/")
	s.run("admin login submit",
		chromedp.SendKeys("#field-username", smokeAdminUser),
		chromedp.SendKeys("#field-password", smokeAdminPassword),
		chromedp.Submit("#field-password"),
		chromedp.WaitVisible("#sidebar"),
	)
	s.visit("admin dashboard", "/admin/")
	s.visit("admin changelist", "/admin/smoke_note/")
	s.adminNavToggle()
	s.visit("admin change form", "/admin/smoke_note/1/")
	s.adminMarkdownPreview()

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
