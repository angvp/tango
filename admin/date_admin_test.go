package admin_test

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/i18n/i18ntest"
	"github.com/angvp/tango/model"
)

type dateCrudEvent struct {
	ID       int64 `tango:"pk"`
	Name     string
	Day      model.Date
	StartsAt time.Time
}

const dateEventPath = "/admin/date_crud_event/"

func buildDateEventAdmin(t *testing.T, adminOpts ...admin.Option) (http.Handler, *sql.DB) {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Models().Register(dateCrudEvent{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(dateCrudEvent{}, admin.Options{ListDisplay: []string{"Name", "Day"}}); err != nil {
		t.Fatalf("register admin model: %v", err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store, adminOpts...)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, sqlDB
}

func countDateEvents(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM date_crud_event`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestDateFieldRendersAsADateInputAndTimeStaysDatetimeLocal(t *testing.T) {
	handler, _ := buildDateEventAdmin(t)
	response := doRequest(t, handler, http.MethodGet, dateEventPath+"new/", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, `type="date" name="Day"`) {
		t.Errorf("the Day field does not render as an input of type date")
	}
	if !strings.Contains(body, `type="datetime-local" name="StartsAt"`) {
		t.Errorf("the time.Time field no longer renders as datetime-local")
	}
}

func TestDateFieldSavesAValidValueAndAnEmptyOneAsZero(t *testing.T) {
	handler, sqlDB := buildDateEventAdmin(t)
	if response := doRequest(t, handler, http.MethodPost, dateEventPath+"new/", url.Values{"Name": {"set"}, "Day": {"2026-03-01"}}); response.Code != http.StatusFound {
		t.Fatalf("valid date: status = %d, want 302", response.Code)
	}
	if response := doRequest(t, handler, http.MethodPost, dateEventPath+"new/", url.Values{"Name": {"unset"}, "Day": {""}}); response.Code != http.StatusFound {
		t.Fatalf("empty date: status = %d, want 302", response.Code)
	}
	var stored model.Date
	if err := sqlDB.QueryRow(`SELECT day FROM date_crud_event WHERE name = 'set'`).Scan(&stored); err != nil || stored != model.MustDate(2026, time.March, 1) {
		t.Fatalf("stored day = %v, %v; want 2026-03-01", stored, err)
	}
	var nulls int
	if err := sqlDB.QueryRow(`SELECT CASE WHEN day IS NULL THEN 1 ELSE 0 END FROM date_crud_event WHERE name = 'unset'`).Scan(&nulls); err != nil || nulls != 1 {
		t.Fatalf("empty date is not stored as NULL: %d, %v", nulls, err)
	}
}

func TestDateFieldRejectsBadValuesWithTheDateMessageAndSavesNothing(t *testing.T) {
	handler, sqlDB := buildDateEventAdmin(t)
	for _, bad := range []string{"2026-02-30", "2026-1-2", "2026-01-02T10:00"} {
		response := doRequest(t, handler, http.MethodPost, dateEventPath+"new/", url.Values{"Name": {"bad"}, "Day": {bad}})
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("%q: status = %d, want 422", bad, response.Code)
		}
		if !strings.Contains(response.Body.String(), "Enter a date as YYYY-MM-DD.") {
			t.Errorf("%q: the Date-specific message is missing", bad)
		}
		if strings.Contains(response.Body.String(), "Enter a date and time.") {
			t.Errorf("%q: the datetime message was shown for a date field", bad)
		}
	}
	if n := countDateEvents(t, sqlDB); n != 0 {
		t.Errorf("%d rows saved from rejected input, want 0", n)
	}
}

func TestDateFieldShowsYYYYMMDDInTheChangelist(t *testing.T) {
	handler, sqlDB := buildDateEventAdmin(t)
	if _, err := sqlDB.Exec(`INSERT INTO date_crud_event (name, day) VALUES ('listed', '2026-03-01')`); err != nil {
		t.Fatal(err)
	}
	response := doRequest(t, handler, http.MethodGet, dateEventPath, nil)
	if !strings.Contains(response.Body.String(), "2026-03-01") {
		t.Errorf("the changelist does not show the date as YYYY-MM-DD")
	}
}

// A date is not a filter: a Date in Filters is refused at registration with
// the same message any non-boolean, non-foreign-key field gets.
func TestDateInListFiltersIsRefusedAtRegistration(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Models().Register(dateCrudEvent{}); err != nil {
		t.Fatal(err)
	}
	err := registry.Admin().Register(dateCrudEvent{}, admin.Options{Filters: []string{"Day"}})
	if err == nil {
		err = registry.RunRegistration()
	}
	if err == nil || !strings.Contains(err.Error(), "must be a boolean or foreign key") {
		t.Errorf("Day in Filters: err = %v, want the boolean-or-foreign-key refusal", err)
	}
}

// The Date message is a translatable key: with a catalog registered for the
// request's locale, a bad date shows the catalog's text.
func TestDateErrorTranslatesWithI18nMiddleware(t *testing.T) {
	i18ntest.Isolate(t)
	handler, _ := buildDateEventAdmin(t, admin.WithMiddleware(adminI18NMiddleware()))
	i18n.RegisterCatalog(adminI18NLocale, map[string]string{
		"admin.error.not_date": "Escribe una fecha con el formato AAAA-MM-DD.",
	})
	response := doRequest(t, handler, http.MethodPost, dateEventPath+"new/", url.Values{"Name": {"bad"}, "Day": {"2026-02-30"}})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.Code)
	}
	if !strings.Contains(response.Body.String(), "Escribe una fecha con el formato AAAA-MM-DD.") {
		t.Errorf("the translated Date message is missing from the form")
	}
}
