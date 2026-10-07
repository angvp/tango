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
	_ "modernc.org/sqlite"
)

// widgetGadget exercises every built-in Widget/parsing branch crudProduct's
// string/float-only fields never reach: a bool field (checkboxWidget), an
// int field and a uint field (setFieldFromString's integer branches), and a
// time.Time field (setFieldFromString's and formatFieldValue's struct
// branches, and inputTypeForKind's "datetime-local" case).
type widgetGadget struct {
	ID       int64 `tango:"pk"`
	Active   bool
	Count    int
	Score    uint
	StartsAt time.Time
}

const gadgetBasePath = "/admin/widget_gadget/"

func buildGadgetAdmin(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()

	registry := tango.NewRegistry()
	if err := registry.Models().Register(widgetGadget{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(widgetGadget{}, admin.Options{
		ListDisplay: []string{"Active", "Count", "Score"},
	}); err != nil {
		t.Fatalf("register admin model: %v", err)
	}

	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store)); err != nil {
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

func readGadget(t *testing.T, sqlDB *sql.DB, id int64) widgetGadget {
	t.Helper()
	var g widgetGadget
	var startsAt string
	if err := sqlDB.QueryRow(`SELECT id, active, count, score, starts_at FROM widget_gadget WHERE id = $1`, id).
		Scan(&g.ID, &g.Active, &g.Count, &g.Score, &startsAt); err != nil {
		t.Fatalf("read gadget: %v", err)
	}
	return g
}

func TestCreateViewRendersUncheckedCheckboxForZeroValueBoolField(t *testing.T) {
	handler, _ := buildGadgetAdmin(t)

	body := doRequest(t, handler, "GET", gadgetBasePath+"new/", nil).Body.String()
	if !strings.Contains(body, `type="checkbox"`) {
		t.Fatalf("expected a checkbox input for the bool field:\n%s", body)
	}
	if strings.Contains(body, `name="Active" `+"checked") {
		t.Fatalf("checkbox for a zero-value bool field must not be pre-checked:\n%s", body)
	}
}

func TestCreateViewCheckboxPresentOrAbsentSetsBoolField(t *testing.T) {
	handler, sqlDB := buildGadgetAdmin(t)

	// Present ("Active=on") means checked; a submitted form that omits the
	// field entirely (as an unchecked HTML checkbox does) means false.
	checked := doRequest(t, handler, "POST", gadgetBasePath+"new/", url.Values{
		"Active": {"on"}, "Count": {"1"}, "Score": {"1"}, "StartsAt": {""},
	})
	if checked.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", checked.Code, http.StatusFound, checked.Body.String())
	}
	if !readGadget(t, sqlDB, 1).Active {
		t.Fatal("Active = false, want true when the checkbox field was submitted present")
	}

	unchecked := doRequest(t, handler, "POST", gadgetBasePath+"new/", url.Values{
		"Count": {"1"}, "Score": {"1"}, "StartsAt": {""},
	})
	if unchecked.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", unchecked.Code, http.StatusFound, unchecked.Body.String())
	}
	if readGadget(t, sqlDB, 2).Active {
		t.Fatal("Active = true, want false when the checkbox field was omitted from the submission")
	}
}

func TestCreateViewMalformedIntFieldReRendersWithError(t *testing.T) {
	handler, sqlDB := buildGadgetAdmin(t)

	response := doRequest(t, handler, "POST", gadgetBasePath+"new/", url.Values{
		"Count": {"not-a-number"}, "Score": {"1"}, "StartsAt": {""},
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM widget_gadget`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 0 {
		t.Fatal("a row was created despite a malformed int field")
	}
}

func TestCreateViewMalformedUintFieldReRendersWithError(t *testing.T) {
	handler, _ := buildGadgetAdmin(t)

	response := doRequest(t, handler, "POST", gadgetBasePath+"new/", url.Values{
		"Count": {"1"}, "Score": {"-1"}, "StartsAt": {""},
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
}

func TestCreateViewValidUintFieldIsStored(t *testing.T) {
	handler, sqlDB := buildGadgetAdmin(t)

	response := doRequest(t, handler, "POST", gadgetBasePath+"new/", url.Values{
		"Count": {"1"}, "Score": {"42"}, "StartsAt": {""},
	})
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusFound, response.Body.String())
	}
	if got := readGadget(t, sqlDB, 1).Score; got != 42 {
		t.Fatalf("Score = %d, want 42", got)
	}
}

func TestCreateViewMalformedTimeFieldReRendersWithError(t *testing.T) {
	handler, _ := buildGadgetAdmin(t)

	response := doRequest(t, handler, "POST", gadgetBasePath+"new/", url.Values{
		"Count": {"1"}, "Score": {"1"}, "StartsAt": {"not-a-date"},
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusUnprocessableEntity, response.Body.String())
	}
}

func TestEditViewPrefillsTimeFieldAndBlankTimeFieldRendersEmpty(t *testing.T) {
	handler, sqlDB := buildGadgetAdmin(t)

	withTime := doRequest(t, handler, "POST", gadgetBasePath+"new/", url.Values{
		"Count": {"1"}, "Score": {"1"}, "StartsAt": {"2024-01-02T15:04"},
	})
	if withTime.Code != http.StatusFound {
		t.Fatalf("create with a valid time: status = %d, body: %s", withTime.Code, withTime.Body.String())
	}

	withoutTime := doRequest(t, handler, "POST", gadgetBasePath+"new/", url.Values{
		"Count": {"1"}, "Score": {"1"}, "StartsAt": {""},
	})
	if withoutTime.Code != http.StatusFound {
		t.Fatalf("create with a blank time: status = %d, body: %s", withoutTime.Code, withoutTime.Body.String())
	}

	_ = sqlDB
	editWithTime := doRequest(t, handler, "GET", gadgetBasePath+"1/", nil).Body.String()
	if !strings.Contains(editWithTime, `value="2024-01-02T15:04"`) {
		t.Fatalf("edit form does not prefill the stored time value:\n%s", editWithTime)
	}

	editWithoutTime := doRequest(t, handler, "GET", gadgetBasePath+"2/", nil).Body.String()
	if !strings.Contains(editWithoutTime, `name="StartsAt" value=""`) {
		t.Fatalf("edit form for a zero-value time field should render an empty value, got:\n%s", editWithoutTime)
	}
}
