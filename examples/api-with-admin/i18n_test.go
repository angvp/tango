package main

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/i18n/i18ntest"
	"github.com/angvp/tango/model"

	"api-with-admin/apps/authors"
	"api-with-admin/apps/posts"
)

// adminSite is the example's admin on a real server, with a signed-in
// browser, an author and a post to look at.
type adminSite struct {
	t        *testing.T
	url      string
	browser  *http.Client
	postID   int64
	authorID int64
	store    *db.Store
	postMeta model.ModelMeta
}

func newAdminSite(t *testing.T) *adminSite {
	t.Helper()
	handler, store, registry := newTestServer(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	authorMeta, _ := registry.Models().Get("Author")
	author := authors.Author{Name: "Ada", Email: "ada@example.com"}
	if err := store.Create(t.Context(), authorMeta, &author); err != nil {
		t.Fatal(err)
	}
	postMeta, _ := registry.Models().Get("Post")
	post := posts.Post{Title: "Hola", Body: "Primera", AuthorID: author.ID}
	if err := store.Create(t.Context(), postMeta, &post); err != nil {
		t.Fatal(err)
	}
	if err := admin.CreateAccount(t.Context(), store, "editor", "correct-password"); err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	site := &adminSite{t: t, url: server.URL, postID: post.ID, authorID: author.ID, store: store, postMeta: postMeta,
		browser: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}

	login := site.get("/admin/login/", nil)
	token := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(login.body)
	if token == nil {
		t.Fatalf("no CSRF token on the login page:\n%s", login.body)
	}
	response, err := site.browser.PostForm(server.URL+"/admin/login/", url.Values{
		"username": {"editor"}, "password": {"correct-password"}, "csrf_token": {token[1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("admin login = %d", response.StatusCode)
	}
	return site
}

type page struct {
	status int
	header http.Header
	body   string
}

func (s *adminSite) get(path string, header map[string]string) page {
	s.t.Helper()
	request, err := http.NewRequest(http.MethodGet, s.url+path, nil)
	if err != nil {
		s.t.Fatal(err)
	}
	for name, value := range header {
		request.Header.Set(name, value)
	}
	response, err := s.browser.Do(request)
	if err != nil {
		s.t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		s.t.Fatal(err)
	}
	return page{status: response.StatusCode, header: response.Header, body: string(body)}
}

// walk visits the admin pages a Spanish-speaking editor sees, each requested
// in locale through the Accept-Language header.
func (s *adminSite) walk(locale string) map[string]page {
	s.t.Helper()
	id := itoa(s.postID)
	paths := map[string]string{
		"dashboard":  "/admin/",
		"changelist": "/admin/post/",
		"search":     "/admin/post/?q=Hola",
		"no results": "/admin/post/?q=zzz",
		"create":     "/admin/post/new/",
		"edit":       "/admin/post/" + id + "/",
		"delete":     "/admin/post/" + id + "/delete/",
		"authors":    "/admin/author/",
		"login":      "/admin/login/",
	}
	header := map[string]string{"Accept-Language": locale}
	pages := make(map[string]page, len(paths))
	for name, path := range paths {
		pages[name] = s.get(path, header)
	}
	for name, outcome := range s.outcomes(header) {
		pages[name] = outcome
	}
	return pages
}

// outcomes runs the create, save and delete flows through the browser and
// returns the changelist each one redirects to, so every outcome message is
// rendered in the locale the walk asked for.
func (s *adminSite) outcomes(header map[string]string) map[string]page {
	s.t.Helper()
	author := strconv.FormatInt(s.authorID, 10)
	s.submitTo("/admin/post/new/", "/admin/post/new/", url.Values{"Title": {"Nuevo"}, "Body": {"Texto"}, "AuthorID": {author}}, header)
	created := s.get("/admin/post/", header)

	s.submitTo("/admin/post/"+itoa(s.postID)+"/", "/admin/post/"+itoa(s.postID)+"/", url.Values{"Title": {"Hola"}, "Body": {"Primera"}, "AuthorID": {author}}, header)
	saved := s.get("/admin/post/", header)

	single := s.seedPost("Borrador")
	s.submitTo("/admin/post/"+itoa(single)+"/delete/", "/admin/post/"+itoa(single)+"/delete/", nil, header)
	removed := s.get("/admin/post/", header)

	one := s.seedPost("Uno")
	s.submitTo("/admin/post/", "/admin/post/delete-selected/", url.Values{"pk": {itoa(one)}, "count": {"1"}}, header)
	deleted := s.get("/admin/post/", header)

	first, second := s.seedPost("Dos"), s.seedPost("Tres")
	s.submitTo("/admin/post/", "/admin/post/delete-selected/", url.Values{"pk": {itoa(first), itoa(second)}, "count": {"2"}}, header)
	deletedMany := s.get("/admin/post/", header)

	return map[string]page{"created": created, "saved": saved, "removed": removed, "deleted": deleted, "deleted n": deletedMany}
}

// seedPost creates a post under the example's author and returns its key.
func (s *adminSite) seedPost(title string) int64 {
	s.t.Helper()
	post := posts.Post{Title: title, Body: "Texto", AuthorID: s.authorID}
	if err := s.store.Create(s.t.Context(), s.postMeta, &post); err != nil {
		s.t.Fatal(err)
	}
	return post.ID
}

// submitTo loads formPath with a GET, then posts fields and the CSRF token
// that page carries to action. The redirect is not followed; the browser keeps
// the cookies it sets.
func (s *adminSite) submitTo(formPath, action string, fields url.Values, header map[string]string) page {
	s.t.Helper()
	loaded := s.get(formPath, header)
	match := csrfTokenPattern.FindStringSubmatch(loaded.body)
	if match == nil {
		s.t.Fatalf("GET %s: no CSRF token on the page", formPath)
	}
	form := url.Values{"csrf_token": {match[1]}}
	for name, values := range fields {
		form[name] = values
	}
	request, err := http.NewRequest(http.MethodPost, s.url+action, strings.NewReader(form.Encode()))
	if err != nil {
		s.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, value := range header {
		request.Header.Set(name, value)
	}
	response, err := s.browser.Do(request)
	if err != nil {
		s.t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		s.t.Fatal(err)
	}
	return page{status: response.StatusCode, header: response.Header, body: string(body)}
}

var csrfTokenPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestSpanishAdminIsComplete(t *testing.T) {
	i18ntest.Isolate(t)
	var mu sync.Mutex
	var missing []string
	i18n.OnMissing(func(locale, key string) {
		mu.Lock()
		defer mu.Unlock()
		missing = append(missing, locale+" "+key)
	})
	site := newAdminSite(t)

	for _, locale := range []string{"es", "es-MX"} {
		for name, got := range site.walk(locale) {
			if got.status >= http.StatusInternalServerError {
				t.Errorf("%s %s: status %d", locale, name, got.status)
			}
			if !strings.Contains(got.body, `<html lang="`+locale+`"`) {
				t.Errorf("%s %s: no lang=%q", locale, name, locale)
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(missing) > 0 {
		t.Errorf("keys the admin used that the catalogs lack: %v", missing)
	}
	report := i18n.Validate()
	for _, problem := range report.Problems {
		t.Errorf("catalog problem: %+v", problem)
	}
	for _, key := range report.Unobserved {
		if !unwalkedKey(key.Key) {
			t.Errorf("catalog key nothing in the admin uses: %+v", key)
		}
	}
}

// unwalkedKey reports whether key belongs to admin behaviour the example's
// models do not reach: the Markdown and JSON widgets and their preview
// endpoint (Post has no such field), a failed system check or a job run, and
// states the walk does not produce (a non-staff login, a bad theme or method).
func unwalkedKey(key string) bool {
	for _, prefix := range []string{
		"admin.json.", "admin.markdown.", "admin.widget.", "admin.error.", "admin.dashboard.activity.job.",
		"admin.dashboard.status.unreachable", "admin.dashboard.status.missing", "admin.dashboard.status.pending",
		"admin.dashboard.status.unknown", "admin.dashboard.unavailable", "admin.dashboard.not_configured",
		"admin.list.filter_too_many", "admin.index.empty", "admin.login.invalid",
		// Data the example does not have: boolean columns, filters, a second
		// page of rows, an empty model, and traffic recorded by a Window.
		"admin.boolean.", "admin.bulk.back_to", "admin.list.filter_any", "admin.list.empty_first", "admin.list.empty_title", "admin.list.empty_no_filter_match", "admin.list.showing", "admin.list.count_one", "admin.list.count_many", "admin.list.filters_applied", "admin.list.filter_pair", "admin.list.clear_filters",
		"admin.pagination.", "admin.dashboard.activity.latency", "admin.dashboard.activity.samples",
		"admin.dashboard.activity.total", "admin.dashboard.status.not_reported",
	} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func TestSpanishAdminPagesReadAsSpanish(t *testing.T) {
	i18ntest.Isolate(t)
	site := newAdminSite(t)
	pages := site.walk("es")
	for name, want := range map[string]string{
		"login":      "Iniciar sesión",
		"dashboard":  "Panel",
		"changelist": "Buscar",
		"create":     "Guardar",
		"delete":     "Esta acción no se puede deshacer.",
		"deleted":    "Se eliminó 1 post.",
		"deleted n":  "Se eliminaron 2 posts.",
		"no results": "No hay posts que coincidan con",
	} {
		if !strings.Contains(pages[name].body, want) {
			t.Errorf("%s page lacks %q", name, want)
		}
	}
	if strings.Contains(pages["create"].body, ">Save<") {
		t.Error("the create page still shows English")
	}
	if !strings.Contains(pages["changelist"].body, "Añadir post") {
		t.Error("the es add button is not Spanish")
	}
}

func TestMexicanSpanishOverridesAFewKeysAndFallsBackToEs(t *testing.T) {
	i18ntest.Isolate(t)
	site := newAdminSite(t)
	mx := site.walk("es-MX")["changelist"].body
	if !strings.Contains(mx, "Agregar post") || strings.Contains(mx, "Añadir post") {
		t.Error("es-MX add button not overridden")
	}
	if !strings.Contains(mx, ">Buscar<") {
		t.Errorf("es-MX did not fall back to es for the search button:\n%s", mx)
	}
}

func TestQueryOverridesTheCookieForOneRequestAndTheCookiePersists(t *testing.T) {
	i18ntest.Isolate(t)
	site := newAdminSite(t)

	// POST /language/ remembers the choice.
	response, err := site.browser.PostForm(site.url+"/language/", url.Values{"lang": {"es"}, "next": {"/admin/post/"}})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound || response.Header.Get("Location") != "/admin/post/" {
		t.Fatalf("switch = %d to %q", response.StatusCode, response.Header.Get("Location"))
	}
	jarCookies := site.browser.Jar.Cookies(mustURL(t, site.url))
	var remembered bool
	for _, c := range jarCookies {
		remembered = remembered || (c.Name == "lang" && c.Value == "es")
	}
	if !remembered {
		t.Fatalf("lang cookie not stored: %v", jarCookies)
	}

	// The cookie wins over Accept-Language, and a query beats the cookie
	// for that request only.
	english := map[string]string{"Accept-Language": "en"}
	if body := site.get("/admin/post/", english).body; !strings.Contains(body, `lang="es"`) {
		t.Errorf("the cookie did not select Spanish:\n%.300s", body)
	}
	if body := site.get("/admin/post/?lang=es-MX", english).body; !strings.Contains(body, `lang="es-MX"`) || !strings.Contains(body, "Agregar post") {
		t.Errorf("?lang=es-MX did not override the cookie:\n%.300s", body)
	}
	if body := site.get("/admin/post/", english).body; !strings.Contains(body, `lang="es"`) {
		t.Errorf("the query changed what the cookie remembers:\n%.300s", body)
	}
}

func TestLanguageSwitcherIgnoresUnknownLanguagesAndOffSiteRedirects(t *testing.T) {
	i18ntest.Isolate(t)
	site := newAdminSite(t)
	for _, next := range []string{"https://evil.example/", "//evil.example/", `/\evil.example`} {
		response, err := site.browser.PostForm(site.url+"/language/", url.Values{"lang": {"fr"}, "next": {next}})
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if got := response.Header.Get("Location"); got != "/" {
			t.Errorf("next %q redirected to %q, want /", next, got)
		}
		if len(response.Cookies()) != 0 {
			t.Errorf("an unknown language set a cookie: %v", response.Cookies())
		}
	}
}

func TestResponsesNameTheirLanguageAndWhatTheyVaryOn(t *testing.T) {
	i18ntest.Isolate(t)
	site := newAdminSite(t)

	es := site.get("/admin/post/", map[string]string{"Accept-Language": "es-MX"})
	if got := es.header.Get("Content-Language"); got != "es-MX" {
		t.Errorf("Content-Language = %q, want es-MX", got)
	}
	vary := strings.Join(es.header.Values("Vary"), ",")
	for _, token := range []string{"Accept-Language", "Cookie"} {
		if !strings.Contains(vary, token) {
			t.Errorf("Vary = %q, missing %s", vary, token)
		}
	}

	en := site.get("/admin/post/", map[string]string{"Accept-Language": "en"})
	if got := en.header.Get("Content-Language"); got != "" {
		t.Errorf("English got Content-Language %q", got)
	}
	if !strings.Contains(en.body, `<html lang="en"`) {
		t.Errorf("English page lang:\n%.200s", en.body)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestAValidationErrorIsShownInSpanish(t *testing.T) {
	i18ntest.Isolate(t)
	site := newAdminSite(t)
	form := site.get("/admin/post/new/", map[string]string{"Accept-Language": "es"})
	token := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(form.body)
	if token == nil {
		t.Fatal("no CSRF token on the create form")
	}
	request, err := http.NewRequest(http.MethodPost, site.url+"/admin/post/new/", strings.NewReader(url.Values{
		"Title": {"x"}, "Body": {"y"}, "AuthorID": {"not-a-number"}, "csrf_token": {token[1]},
	}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept-Language", "es")
	response, err := site.browser.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode < 400 {
		t.Fatalf("status = %d, want a validation error", response.StatusCode)
	}
	if !strings.Contains(string(body), `<html lang="es"`) || strings.Contains(string(body), ">Save<") {
		t.Errorf("the error page is not Spanish (status %d)", response.StatusCode)
	}
}
