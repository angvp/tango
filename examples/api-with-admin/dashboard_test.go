package main

import (
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/observability"
)

// TestTheDashboardShowsTheRealDatabaseAndTheRecentActivity is the example's
// operator workflow: after the migrations ran and some requests were
// recorded, staff opening /admin/ see the database's real state (read with
// tango.Status) and the activity the Window counted, labelled with its scope.
func TestTheDashboardShowsTheRealDatabaseAndTheRecentActivity(t *testing.T) {
	window := observability.NewWindow()
	handler, store, _ := newTestServerWith(t, window)
	// These observations stand in for what tango.ServeContext records for every
	// request it serves: the test builds the handler without ServeContext, so
	// the recording path itself is covered by the root package's metrics tests.
	for _, status := range []int{200, 200, 404, 500} {
		window.ObserveHistogram(observability.MetricHTTPRequestDuration, 0.012,
			slog.String("route", "/posts/"), slog.String("method", "GET"), slog.Int("status", status))
	}
	if err := admin.CreateAccount(t.Context(), store, "operator", "correct-password"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	loginPage := fetch(t, browser, server.URL+"/admin/login/")
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(loginPage)
	if csrf == nil {
		t.Fatalf("the admin login page has no CSRF token:\n%s", loginPage)
	}
	loggedIn, err := browser.PostForm(server.URL+"/admin/login/", url.Values{
		"username": {"operator"}, "password": {"correct-password"}, "csrf_token": {csrf[1]},
	})
	if err != nil {
		t.Fatal(err)
	}
	loggedIn.Body.Close()

	body := fetch(t, browser, server.URL+"/admin/")
	for _, want := range []string{
		`data-dashboard="status"`, `data-state="ready"`, "Reachable", "None pending", "Present",
		"this instance only", "4 samples",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard lacks %q:\n%s", want, body)
		}
	}
}
