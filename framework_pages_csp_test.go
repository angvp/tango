package tango_test

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"

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

// strictReferencePolicy is the policy every framework page is checked
// against. It is written here, not read from the admin, so the admin's own
// policy value cannot relax the check it is measured by.
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

// strictReferenceHeader is strictReferencePolicy rendered in the package's
// fixed directive order.
const strictReferenceHeader = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; form-action 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"

// adminPolicyHeader is admin.ContentSecurityPolicy rendered in the package's
// fixed directive order. The admin tier replaces the global reference on
// every admin path, so an admin page is checked against this header.
const adminPolicyHeader = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; form-action 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"

const (
	kindInlineScript   = "inline-script"
	kindInlineStyle    = "inline-style"
	kindStyleAttribute = "style-attribute"
	kindEventHandler   = "event-handler"
	kindExternalOrigin = "external-origin"
)

// redListEntry is one known violation: the page it occurs on and its kind.
type redListEntry struct {
	Page string
	Kind string
}

// frameworkPageRedList names each violation the framework's pages still
// have under the strict reference policy. An entry is removed when its page
// stops producing that kind, and the test fails while it still lists one
// that no longer occurs.
var frameworkPageRedList = []redListEntry{}

// cspPageNote is the one application model in the composed app. It gives the
// admin a changelist, a form and a Markdown preview to render.
type cspPageNote struct {
	ID    int64 `tango:"pk"`
	Title string
	Body  string
}

const (
	cspAdminUser     = "admin"
	cspAdminPassword = "secret"
	cspRequestHost   = "example.com"
)

func cspPreview(_ context.Context, source string) (template.HTML, error) {
	return template.HTML("<p>" + template.HTMLEscapeString(source) + "</p>"), nil
}

// notesApp registers cspPageNote with the admin. It is installed before the
// admin, which lists models registered before it.
func notesApp() tango.App {
	return tango.NewApp("notes", func(registry *tango.Registry) error {
		if err := registry.Models().Register(cspPageNote{}); err != nil {
			return err
		}
		return registry.Admin().Register(cspPageNote{}, admin.Options{
			ListDisplay: []string{"Title"},
			Search:      []string{"Title"},
			BulkDelete:  true,
			Widgets: map[string]admin.Widget{
				"Body": admin.Markdown(admin.WithMarkdownPreview(cspPreview)),
			},
		})
	})
}

// cspViolation is one place a page breaks the strict reference policy.
type cspViolation struct {
	Page   string
	Kind   string
	Detail string
}

// framePageAudit composes the framework's apps, logs in through the admin's
// own login route, and records every violation the pages produce.
type framePageAudit struct {
	t          *testing.T
	handler    http.Handler
	routes     []tango.DescribedRoute
	session    *http.Cookie
	csrf       string
	visited    map[string]bool
	violations []cspViolation
	// stylesheets maps each stylesheet URL a page links, resolved against the
	// page's path, to the page that links it.
	stylesheets map[string]string
}

func TestFrameworkPagesSatisfyTheStrictReferencePolicy(t *testing.T) {
	audit := newFramePageAudit(t)
	audit.loginAdmin()

	for _, route := range audit.routes {
		if route.Method != http.MethodGet {
			continue
		}
		page := "GET " + route.Pattern
		path := fillPattern(route.Pattern)
		audit.visit(page, http.MethodGet, path, "", nil, nil)
		if isAdminPage(path) {
			audit.visit(page, http.MethodGet, path, "", audit.session, nil)
		}
	}
	audit.submitErrorStates()
	audit.previewMarkdown()
	audit.checkLinkedStylesheets()

	for _, page := range []string{
		"GET /admin/",
		"GET /admin/login/",
		"GET /admin/csp_page_note/",
		"GET /admin/csp_page_note/{pk}/",
		"GET /admin/csp_page_note/{pk}/delete/",
		"POST /admin/csp_page_note/delete-selected/",
		"GET /accounts/login/",
		"GET /accounts/register/",
		"GET /accounts/password-reset/",
		"GET /docs/",
		"POST /admin/csp_page_note/_preview/{field}/",
		"POST /admin/login/",
		"POST /accounts/login/",
		"POST /accounts/register/",
		"POST /accounts/password-reset/",
	} {
		if !audit.visited[page] {
			t.Errorf("page %q was never reached as HTML; the route walk did not find it", page)
		}
	}
	audit.compareWithRedList()
}

func newFramePageAudit(t *testing.T) *framePageAudit {
	t.Helper()
	models := model.NewRegistry()
	for _, value := range []any{
		admin.AdminUser{}, admin.AdminSession{},
		accounts.Account{}, accounts.AccountSession{}, accounts.AccountToken{},
		cspPageNote{},
	} {
		if err := models.Register(value); err != nil {
			t.Fatalf("register %T: %v", value, err)
		}
	}
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, models.All())
	store := db.NewStore(sqlDB, dialect)
	if err := admin.CreateAccount(context.Background(), store, cspAdminUser, cspAdminPassword); err != nil {
		t.Fatalf("seed admin account: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO csp_page_note (id, title, body) VALUES (1, 'Hello', 'Body')`); err != nil {
		t.Fatalf("seed note: %v", err)
	}

	config := tango.Config{
		InstalledApps: []tango.App{
			notesApp(),
			accounts.New(store, accounts.WithMail(accounts.MailConfig{
				Sender:  &mailtest.Sender{},
				From:    "Shop <noreply@example.com>",
				BaseURL: "https://" + cspRequestHost,
			})),
			openapi.New(openapi.Options{UI: swaggerui.New(), Path: "/openapi.json"}),
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
	return &framePageAudit{
		t:           t,
		handler:     handler,
		routes:      registry.Routes().Described(),
		visited:     map[string]bool{},
		stylesheets: map[string]string{},
	}
}

// loginAdmin signs in through GET and POST /admin/login/, the way a browser
// does, and keeps the session cookie for the authenticated visits.
func (a *framePageAudit) loginAdmin() {
	a.t.Helper()
	get := a.request(http.MethodGet, "/admin/login/", "", nil, nil)
	var csrfCookie *http.Cookie
	for _, cookie := range get.Result().Cookies() {
		if cookie.Name == "tango_admin_login_csrf" {
			csrfCookie = cookie
		}
	}
	if csrfCookie == nil {
		a.t.Fatal("GET /admin/login/ set no login CSRF cookie")
	}
	form := url.Values{"username": {cspAdminUser}, "password": {cspAdminPassword}, "csrf_token": {csrfCookie.Value}}
	post := a.request(http.MethodPost, "/admin/login/", form.Encode(), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}, []*http.Cookie{csrfCookie})
	if post.Code != http.StatusFound {
		a.t.Fatalf("admin login status = %d, want 302", post.Code)
	}
	for _, cookie := range post.Result().Cookies() {
		if cookie.Name == "tango_admin_session" {
			a.session = cookie
		}
	}
	if a.session == nil {
		a.t.Fatal("admin login set no session cookie")
	}
}

// previewMarkdown posts to the Markdown preview route the admin mounted for
// cspPageNote's Body, using the CSRF token the rendered admin form carried.
// The preview's JSON html field is then scanned like a page.
func (a *framePageAudit) previewMarkdown() {
	a.t.Helper()
	if a.csrf == "" {
		a.t.Fatal("no admin page carried a csrf_token to send with the preview")
	}
	pattern := ""
	for _, route := range a.routes {
		if route.Method == http.MethodPost && strings.Contains(route.Pattern, "_preview/") {
			pattern = route.Pattern
		}
	}
	if pattern == "" {
		a.t.Fatal("the composed app mounts no Markdown preview route")
	}
	page := "POST " + pattern
	response := a.request(http.MethodPost, fillPattern(pattern), "# Hello", map[string]string{
		"Content-Type": "text/plain; charset=utf-8",
		"X-CSRF-Token": a.csrf,
	}, []*http.Cookie{a.session})
	if response.Code != http.StatusOK {
		a.t.Fatalf("%s status = %d, want 200: %s", page, response.Code, response.Body.String())
	}
	a.checkPolicyHeader(page, pattern, response)
	var payload struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		a.t.Fatalf("%s did not answer JSON: %v", page, err)
	}
	a.visited[page] = true
	a.scan(page, fillPattern(pattern), payload.HTML)
}

// visit requests one page and checks its policy header and, when it is HTML,
// its body.
func (a *framePageAudit) visit(page, method, path, body string, session *http.Cookie, headers map[string]string) {
	a.t.Helper()
	var cookies []*http.Cookie
	if session != nil {
		cookies = []*http.Cookie{session}
	}
	response := a.request(method, path, body, headers, cookies)
	a.checkPolicyHeader(page, path, response)
	if session != nil && method == http.MethodGet && response.Code != http.StatusOK {
		a.t.Errorf("%s: authenticated GET status = %d, want 200", page, response.Code)
	}
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
		return
	}
	a.visited[page] = true
	if session != nil && a.csrf == "" {
		a.csrf = csrfTokenIn(response.Body.String())
	}
	a.scan(page, path, response.Body.String())
}

func (a *framePageAudit) request(method, path, body string, headers map[string]string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	a.handler.ServeHTTP(response, request)
	return response
}

// checkPolicyHeader requires exactly one Content-Security-Policy header on the
// response, and that it is the policy the path is served under.
func (a *framePageAudit) checkPolicyHeader(page, path string, response *httptest.ResponseRecorder) {
	a.t.Helper()
	want := strictReferenceHeader
	if isAdminPath(path) {
		want = adminPolicyHeader
	}
	values := response.Header().Values("Content-Security-Policy")
	if len(values) != 1 || values[0] != want {
		a.t.Errorf("%s: Content-Security-Policy values = %q, want exactly [%q]", page, values, want)
	}
}

// scan records every violation in one HTML document, and each stylesheet it
// links. path is the request path the document was served at.
func (a *framePageAudit) scan(page, path string, body string) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		a.t.Fatalf("%s: parse HTML: %v", page, err)
	}
	base, err := url.Parse(path)
	if err != nil {
		a.t.Fatalf("%s: unparsable request path %q: %v", page, path, err)
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			a.inspectElement(page, n)
			if n.Data == "link" && strings.EqualFold(attributeValue(n, "rel"), "stylesheet") {
				if ref, err := url.Parse(attributeValue(n, "href")); err == nil {
					a.stylesheets[base.ResolveReference(ref).String()] = page
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
}

func (a *framePageAudit) inspectElement(page string, n *html.Node) {
	if n.Data == "script" && attribute(n, "src") == nil {
		a.record(page, kindInlineScript, n)
	}
	if n.Data == "style" {
		a.record(page, kindInlineStyle, n)
	}
	for _, attr := range n.Attr {
		key := strings.ToLower(attr.Key)
		switch {
		case key == "style":
			a.record(page, kindStyleAttribute, n)
		case strings.HasPrefix(key, "on"):
			a.record(page, kindEventHandler, n)
		case key == "src", key == "href":
			if !sameOrigin(attr.Val) {
				a.record(page, kindExternalOrigin, n)
			}
		}
	}
}

func (a *framePageAudit) record(page, kind string, n *html.Node) {
	a.violations = append(a.violations, cspViolation{Page: page, Kind: kind, Detail: openTag(n)})
}

// checkLinkedStylesheets requests every stylesheet an admin or accounts page
// links and requires each to come from the application's own origin as
// text/css with nosniff and an immutable cache lifetime. It fails when either
// family links none, so the check cannot pass on an empty set.
func (a *framePageAudit) checkLinkedStylesheets() {
	a.t.Helper()
	linked := map[string]int{}
	for href, page := range a.stylesheets {
		family := stylesheetFamily(href)
		if family == "" {
			continue
		}
		linked[family]++
		response := a.request(http.MethodGet, href, "", nil, nil)
		if response.Code != http.StatusOK {
			a.t.Errorf("%s links %s: status = %d, want 200", page, href, response.Code)
		}
		wantHeaders := map[string]string{
			"Content-Type":           "text/css; charset=utf-8",
			"X-Content-Type-Options": "nosniff",
			"Cache-Control":          "public, max-age=31536000, immutable",
		}
		for name, want := range wantHeaders {
			if got := response.Header().Get(name); got != want {
				a.t.Errorf("%s links %s: %s = %q, want %q", page, href, name, got, want)
			}
		}
	}
	for _, family := range []string{"accounts", "admin"} {
		if linked[family] == 0 {
			a.t.Errorf("no %s page linked a stylesheet", family)
		}
	}
}

// stylesheetFamily names the framework area whose hashed asset route serves
// href, or "" for a stylesheet outside the framework's own routes.
func stylesheetFamily(href string) string {
	switch {
	case strings.HasPrefix(href, "/accounts/"):
		return "accounts"
	case strings.HasPrefix(href, "/admin/_assets/"):
		return "admin"
	}
	return ""
}

// compareWithRedList fails for a violation the list does not name, and for a
// named violation that no longer occurs. It logs the observed set in the
// form the list is written in, so the list is pasted from a run.
func (a *framePageAudit) compareWithRedList() {
	a.t.Helper()
	observed := map[redListEntry]string{}
	for _, v := range a.violations {
		key := redListEntry{Page: v.Page, Kind: v.Kind}
		if _, seen := observed[key]; !seen {
			observed[key] = v.Detail
		}
	}
	keys := make([]redListEntry, 0, len(observed))
	for key := range observed {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Page != keys[j].Page {
			return keys[i].Page < keys[j].Page
		}
		return keys[i].Kind < keys[j].Kind
	})

	listed := map[redListEntry]bool{}
	for _, entry := range frameworkPageRedList {
		listed[entry] = true
	}

	var lines []string
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("\t{Page: %q, Kind: %q}, // %s", key.Page, key.Kind, observed[key]))
		if !listed[key] {
			a.t.Errorf("new violation not on the red list: %s %s at %s", key.Page, key.Kind, observed[key])
		}
	}
	for _, entry := range frameworkPageRedList {
		if _, ok := observed[entry]; !ok {
			a.t.Errorf("red list entry %s %s no longer occurs; delete it", entry.Page, entry.Kind)
		}
	}
	a.t.Logf("observed violations, paste-ready:\n%s", strings.Join(lines, "\n"))
}

// fillPattern turns a route pattern into a request path: the preview field
// becomes Body, and every other parameter becomes 1, the seeded row's key.
var routePlaceholder = regexp.MustCompile(`\{[^}]*\}`)

func fillPattern(pattern string) string {
	return routePlaceholder.ReplaceAllStringFunc(pattern, func(token string) string {
		if strings.HasPrefix(token, "{field") {
			return "Body"
		}
		return "1"
	})
}

// isAdminPage reports whether a path sits behind the admin session, other
// than the login page itself.
func isAdminPage(path string) bool {
	return isAdminPath(path) && !strings.HasPrefix(path, "/admin/login/")
}

// isAdminPath reports whether a path is served under the admin tier.
func isAdminPath(path string) bool {
	return path == "/admin" || strings.HasPrefix(path, "/admin/")
}

// sameOrigin reports whether a src or href value loads from the test
// application's own origin.
func sameOrigin(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "#") {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.Scheme == "" && parsed.Host == "" {
		return true
	}
	if parsed.Scheme == "" {
		return parsed.Host == cspRequestHost
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host == cspRequestHost
}

func attribute(n *html.Node, name string) *html.Attribute {
	for i := range n.Attr {
		if strings.EqualFold(n.Attr[i].Key, name) {
			return &n.Attr[i]
		}
	}
	return nil
}

func attributeValue(n *html.Node, name string) string {
	if attr := attribute(n, name); attr != nil {
		return attr.Val
	}
	return ""
}

// openTag renders an element's start tag, with its inline text for a script
// or style, so a reported violation can be found in the source.
func openTag(n *html.Node) string {
	var b strings.Builder
	b.WriteString("<" + n.Data)
	for _, attr := range n.Attr {
		b.WriteString(fmt.Sprintf(" %s=%q", attr.Key, attr.Val))
	}
	b.WriteString(">")
	if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
		text := strings.TrimSpace(n.FirstChild.Data)
		if runes := []rune(text); len(runes) > 60 {
			text = string(runes[:60]) + "..."
		}
		b.WriteString(text)
	}
	return b.String()
}

// csrfTokenIn returns the csrf_token a rendered form carries, or "" when the
// document has none. Only an authenticated page's token is the session's.
func csrfTokenIn(body string) string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return ""
	}
	var found string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "input" && attributeValue(n, "name") == "csrf_token" {
			found = attributeValue(n, "value")
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return found
}

// submitErrorStates posts each framework form with values it rejects, so the
// page renders its error branch. A GET never reaches that branch, and an
// error paragraph is part of the markup a strict policy must still allow.
func (a *framePageAudit) submitErrorStates() {
	a.postErrorState("/admin/login/", url.Values{"username": {cspAdminUser}, "password": {"wrong-password"}})
	a.postErrorState("/accounts/login/", url.Values{"email": {"nobody@example.com"}, "password": {"wrong-password"}})
	a.postErrorState("/accounts/register/", url.Values{"email": {"not-an-email"}, "password": {"x"}})
	a.postErrorState("/accounts/password-reset/", url.Values{"email": {"not-an-email"}})
	a.postBulkRejection()
	a.visitErrorPages()
}

// visitErrorPages requests the admin's HTML error pages under the admin
// session: a missing row, and a form posted without its CSRF token.
func (a *framePageAudit) visitErrorPages() {
	a.visitErrorPage("GET /admin/csp_page_note/{pk}/ (missing row)", http.MethodGet, "/admin/csp_page_note/999999/", "", nil, http.StatusNotFound)
	a.visitErrorPage("POST /admin/csp_page_note/new/ (expired form)", http.MethodPost, "/admin/csp_page_note/new/", url.Values{"Body": {"x"}}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, http.StatusForbidden)
}

// visitErrorPage checks an error response's status and scans its markup. The
// route walk's visit expects a 200, so an error page is requested here.
func (a *framePageAudit) visitErrorPage(page, method, path, body string, headers map[string]string, status int) {
	a.t.Helper()
	response := a.request(method, path, body, headers, []*http.Cookie{a.session})
	a.checkPolicyHeader(page, path, response)
	if response.Code != status {
		a.t.Errorf("%s: status = %d, want %d", page, response.Code, status)
	}
	a.scan(page, path, response.Body.String())
}

// postBulkRejection posts an empty bulk selection with the changelist's
// token and the admin session. The bulk route is POST-only, so its token comes
// from the changelist page, and the response is the rejection page.
func (a *framePageAudit) postBulkRejection() {
	a.t.Helper()
	const path = "/admin/csp_page_note/delete-selected/"
	page := "POST " + path
	list := a.request(http.MethodGet, "/admin/csp_page_note/", "", nil, []*http.Cookie{a.session})
	token := csrfTokenIn(list.Body.String())
	if token == "" {
		a.t.Fatalf("%s: the changelist carried no csrf_token", page)
	}
	fields := url.Values{"count": {"0"}, "csrf_token": {token}}
	post := a.request(http.MethodPost, path, fields.Encode(), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}, []*http.Cookie{a.session})
	a.checkPolicyHeader(page, path, post)
	if !strings.HasPrefix(post.Header().Get("Content-Type"), "text/html") {
		a.t.Fatalf("%s: answered %q, want the rejection page", page, post.Header().Get("Content-Type"))
	}
	a.visited[page] = true
	a.scan(page, path, post.Body.String())
}

// postErrorState fetches a form page, then posts fields with the form's own
// csrf_token and the cookies the GET set, and scans the response as the
// page's error state.
func (a *framePageAudit) postErrorState(path string, fields url.Values) {
	a.t.Helper()
	page := "POST " + path
	get := a.request(http.MethodGet, path, "", nil, nil)
	token := csrfTokenIn(get.Body.String())
	if token == "" {
		a.t.Fatalf("%s: GET %s carried no csrf_token", page, path)
	}
	fields.Set("csrf_token", token)
	post := a.request(http.MethodPost, path, fields.Encode(), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}, get.Result().Cookies())
	a.checkPolicyHeader(page, path, post)
	if !strings.HasPrefix(post.Header().Get("Content-Type"), "text/html") {
		a.t.Fatalf("%s: answered %q, want an HTML error page", page, post.Header().Get("Content-Type"))
	}
	a.visited[page] = true
	a.scan(page, path, post.Body.String())
}
