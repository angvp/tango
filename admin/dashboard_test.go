package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/observability"
)

const secretSentinel = "postgres://user:hunter2@db.internal/app"

func buildDashboardHandler(t *testing.T, opts ...admin.Option) http.Handler {
	t.Helper()
	registry := tango.NewRegistry()
	_, store := migratedAdminDB(t, registry)
	if err := admin.CreateAccount(context.Background(), store, "admin", "correct-password"); err != nil {
		t.Fatalf("seed admin account: %v", err)
	}
	if err := registry.Register(admin.New(store, opts...)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler
}

func getIndex(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	cookie := loginAndGetSessionCookie(t, handler, "admin", "correct-password")
	request := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type stubSource struct {
	snapshot observability.Snapshot
	err      error
	panics   bool
}

func (s stubSource) Snapshot() (observability.Snapshot, error) {
	if s.panics {
		panic(secretSentinel)
	}
	return s.snapshot, s.err
}

func TestDashboardWithoutASourceLeavesTheLandingPageAlone(t *testing.T) {
	for name, opts := range map[string][]admin.Option{
		"no option":       nil,
		"empty dashboard": {admin.WithDashboard(admin.DashboardConfig{})},
	} {
		t.Run(name, func(t *testing.T) {
			response := getIndex(t, buildDashboardHandler(t, opts...))
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "No models registered yet") {
				t.Fatalf("status %d, body %q: want today's empty state", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), `data-dashboard`) {
				t.Fatal("an unconfigured dashboard rendered dashboard markup")
			}
		})
	}
}

func TestConfiguredDashboardRendersCardsForStaff(t *testing.T) {
	handler := buildDashboardHandler(t, admin.WithDashboard(admin.DashboardConfig{
		Activity: stubSource{},
		Status: func(context.Context) (admin.DashboardStatus, error) {
			return admin.DashboardStatus{Dialect: "sqlite"}, nil
		},
	}))
	response := getIndex(t, handler)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	for _, slot := range []string{`data-dashboard="activity"`, `data-dashboard="status"`} {
		if !strings.Contains(body, slot) {
			t.Errorf("dashboard page lacks the %s slot", slot)
		}
	}
}

func TestDashboardIsRefusedWithoutASession(t *testing.T) {
	handler := buildDashboardHandler(t, admin.WithDashboard(admin.DashboardConfig{Activity: stubSource{}}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/", nil))
	if response.Code == http.StatusOK || strings.Contains(response.Body.String(), "data-dashboard") {
		t.Fatalf("anonymous request got status %d with dashboard markup", response.Code)
	}
}

func TestFailingCallbacksMarkOnlyTheirOwnCardUnavailable(t *testing.T) {
	cases := map[string]admin.DashboardConfig{
		"errors": {
			Activity: stubSource{err: errors.New(secretSentinel)},
			Status: func(context.Context) (admin.DashboardStatus, error) {
				return admin.DashboardStatus{}, errors.New(secretSentinel)
			},
		},
		"panics": {
			Activity: stubSource{panics: true},
			Status:   func(context.Context) (admin.DashboardStatus, error) { panic(secretSentinel) },
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			response := getIndex(t, buildDashboardHandler(t, admin.WithDashboard(cfg)))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: a failing card must not fail the page", response.Code)
			}
			body := response.Body.String()
			if strings.Contains(body, "hunter2") || strings.Contains(body, secretSentinel) {
				t.Fatal("a callback's error or panic text reached the page")
			}
			if got := strings.Count(body, `data-state="unavailable"`); got != 2 {
				t.Fatalf("unavailable cards = %d, want 2", got)
			}
		})
	}
}

func TestOneFailingCardLeavesTheOtherUsable(t *testing.T) {
	handler := buildDashboardHandler(t, admin.WithDashboard(admin.DashboardConfig{
		Activity: stubSource{panics: true},
		Status: func(context.Context) (admin.DashboardStatus, error) {
			return admin.DashboardStatus{Dialect: "sqlite"}, nil
		},
	}))
	body := getIndex(t, handler).Body.String()
	if strings.Count(body, `data-state="unavailable"`) != 1 {
		t.Fatalf("want exactly the activity card unavailable:\n%s", body)
	}
}
