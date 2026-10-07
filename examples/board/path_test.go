package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/angvp/tango/accounts"
)

// TestTutorialPath walks the tutorial's main path through the real app,
// the way a visitor and an API client would: sign up, log in, post,
// comment, call the API with a token, hit the rate limit, and hear about a
// new post on the live feed. The second pass sends every request as a
// TLS-terminating proxy would forward it, and every cookie the app sets
// must then be Secure.
func TestTutorialPath(t *testing.T) {
	for _, pass := range []struct {
		name  string
		https bool
	}{
		{"plain HTTP", false},
		{"HTTPS behind a proxy", true},
	} {
		t.Run(pass.name, func(t *testing.T) {
			app := newTestApp(t)

			// A visitor has the front page open, listening to the live feed.
			feed := listen(t, app, pass.https)

			// Sign up through the registration form, which logs the new
			// account in.
			signup := newBrowser(t, app, pass.https)
			rec := signup.submit("/accounts/register/", url.Values{
				"email":    {"ana@example.com"},
				"password": {"correct horse battery"},
			})
			if rec.Code != http.StatusFound {
				t.Fatalf("register: status = %d, want 302: %s", rec.Code, rec.Body)
			}
			signup.requireCookie(accounts.DefaultSessionCookieName)

			// Both the CSRF cookie the form set and the session cookie
			// were set, so the Secure check above covered both.
			signup.requireSeen(preSessionCSRFCookie, accounts.DefaultSessionCookieName)

			// Log in from a fresh browser.
			ana := newBrowser(t, app, pass.https)
			rec = ana.submit("/accounts/login/", url.Values{
				"email":    {"ana@example.com"},
				"password": {"correct horse battery"},
			})
			if rec.Code != http.StatusFound {
				t.Fatalf("login: status = %d, want 302: %s", rec.Code, rec.Body)
			}
			ana.requireCookie(accounts.DefaultSessionCookieName)

			// Post through the form, then comment on the post.
			rec = ana.submit("/new/", url.Values{"title": {"Hello, board"}, "body": {"First!"}})
			if rec.Code != http.StatusFound {
				t.Fatalf("new post: status = %d, want 302: %s", rec.Code, rec.Body)
			}
			postPath := rec.Header().Get("Location")
			if postPath != "/p/1/" {
				t.Fatalf("new post redirected to %q, want /p/1/", postPath)
			}
			rec = ana.submitTo(postPath, postPath+"comments/", url.Values{"body": {"Nice to be here"}})
			if rec.Code != http.StatusFound {
				t.Fatalf("comment: status = %d, want 302: %s", rec.Code, rec.Body)
			}
			page := ana.do("GET", postPath, nil).Body.String()
			for _, want := range []string{"Hello, board", "Nice to be here", "ana"} {
				if !strings.Contains(page, want) {
					t.Errorf("post page is missing %q", want)
				}
			}

			// An API client trades the password for a token and posts with it.
			token := app.token(t, "ana@example.com", "correct horse battery")
			rec = app.do("POST", "/posts/", `{"title":"Posted from the API"}`, token)
			if rec.Code != http.StatusCreated {
				t.Fatalf("API post: status = %d, want 201: %s", rec.Code, rec.Body)
			}

			// The token endpoint allows 5 attempts per client IP; the one
			// above was the first, so the sixth is turned away.
			for attempt := 2; attempt <= 5; attempt++ {
				if rec := app.do("POST", "/api/token/", `{"email":"ana@example.com","password":"guess"}`, ""); rec.Code != http.StatusUnauthorized {
					t.Fatalf("attempt %d: status = %d, want 401: %s", attempt, rec.Code, rec.Body)
				}
			}
			rec = app.do("POST", "/api/token/", `{"email":"ana@example.com","password":"correct horse battery"}`, "")
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("attempt 6: status = %d, want 429: %s", rec.Code, rec.Body)
			}

			// The visitor heard about both posts, in order.
			feed.expect(`{"type":"post","id":1,"title":"Hello, board"}`)
			feed.expect(`{"type":"post","id":2,"title":"Posted from the API"}`)
		})
	}
}

// preSessionCSRFCookie is the cookie the accounts app's login and
// registration forms keep their CSRF token in.
const preSessionCSRFCookie = "tango_account_pre_session_csrf"

var csrfField = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// browser sends requests through the app the way a browser would: it
// keeps the cookies the app sets and sends them back. Every cookie it
// receives must be Secure exactly when the browser is on HTTPS.
type browser struct {
	t       *testing.T
	app     *testApp
	https   bool
	cookies map[string]*http.Cookie
	seen    map[string]bool // every cookie name the app has set
}

func newBrowser(t *testing.T, app *testApp, https bool) *browser {
	return &browser{t: t, app: app, https: https, cookies: map[string]*http.Cookie{}, seen: map[string]bool{}}
}

// do sends one request, carrying the browser's cookies, and keeps the
// cookies the response sets.
func (b *browser) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	b.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if b.https {
		req.Header.Set("X-Forwarded-Proto", "https")
	}
	for _, cookie := range b.cookies {
		if strings.HasPrefix(path, cookie.Path) {
			req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
		}
	}
	rec := httptest.NewRecorder()
	b.app.handler.ServeHTTP(rec, req)

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Secure != b.https {
			b.t.Errorf("%s %s set cookie %s with Secure = %t, want %t", method, path, cookie.Name, cookie.Secure, b.https)
		}
		b.seen[cookie.Name] = true
		if cookie.MaxAge < 0 {
			delete(b.cookies, cookie.Name)
		} else {
			b.cookies[cookie.Name] = cookie
		}
	}
	return rec
}

// submit loads the form at path, then posts fields to it along with the
// CSRF token the form carried.
func (b *browser) submit(path string, fields url.Values) *httptest.ResponseRecorder {
	b.t.Helper()
	return b.submitTo(path, path, fields)
}

// submitTo loads the page at formPath and posts fields, with the page's
// CSRF token, to action.
func (b *browser) submitTo(formPath, action string, fields url.Values) *httptest.ResponseRecorder {
	b.t.Helper()
	page := b.do("GET", formPath, nil)
	if page.Code != http.StatusOK {
		b.t.Fatalf("GET %s: status = %d, want 200: %s", formPath, page.Code, page.Body)
	}
	match := csrfField.FindStringSubmatch(page.Body.String())
	if match == nil {
		b.t.Fatalf("GET %s: no csrf_token field in the page", formPath)
	}
	form := url.Values{"csrf_token": {match[1]}}
	for name, values := range fields {
		form[name] = values
	}
	return b.do("POST", action, form)
}

// requireCookie fails the test unless the browser holds the named cookie.
func (b *browser) requireCookie(name string) {
	b.t.Helper()
	if _, ok := b.cookies[name]; !ok {
		b.t.Fatalf("no %s cookie; have %v", name, b.cookies)
	}
}

// feedListener is a visitor's open connection to the live feed.
type feedListener struct {
	t    *testing.T
	conn *websocket.Conn
}

// listen connects to the live feed over a real WebSocket, as the front
// page's script does, and waits for the feed's greeting.
func listen(t *testing.T, app *testApp, https bool) *feedListener {
	t.Helper()
	server := httptest.NewServer(app.handler)
	t.Cleanup(server.Close)

	header := http.Header{}
	if https {
		header.Set("X-Forwarded-Proto", "https")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/live/ws/", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("connect to the live feed: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })

	feed := &feedListener{t: t, conn: conn}
	feed.expect(`{"type":"ready"}`)
	return feed
}

// expect fails the test unless the next message on the feed is want.
func (f *feedListener) expect(want string) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.t.Context(), 5*time.Second)
	defer cancel()
	_, got, err := f.conn.Read(ctx)
	if err != nil {
		f.t.Fatalf("live feed: waiting for %s: %v", want, err)
	}
	if string(got) != want {
		f.t.Fatalf("live feed: got %s, want %s", got, want)
	}
}

// requireSeen fails the test unless the app has set each named cookie in
// this browser at some point.
func (b *browser) requireSeen(names ...string) {
	b.t.Helper()
	for _, name := range names {
		if !b.seen[name] {
			b.t.Errorf("the app never set a %s cookie", name)
		}
	}
}
