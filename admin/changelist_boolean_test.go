package admin_test

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// adminLiveItem has a boolean column, so a changelist test can read the Yes
// and No text a boolean cell shows.
type adminLiveItem struct {
	ID   int64 `tango:"pk"`
	Name string
	Live bool
}

const liveItemBasePath = "/admin/admin_live_item/"

func buildLiveItemAdmin(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminLiveItem{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(adminLiveItem{}, admin.Options{ListDisplay: []string{"Name", "Live"}}); err != nil {
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

// A boolean cell shows the words Yes and No. The badge styling only adds
// emphasis, so the value reads without it.
func TestChangelistBooleanCellsShowYesAndNoAsText(t *testing.T) {
	handler, sqlDB := buildLiveItemAdmin(t)
	if _, err := sqlDB.Exec(`INSERT INTO admin_live_item (name, live) VALUES ($1, $2), ($3, $4)`, "On shelf", true, "Archived", false); err != nil {
		t.Fatalf("seed rows: %v", err)
	}
	body := doRequest(t, handler, http.MethodGet, liveItemBasePath, nil).Body.String()
	for _, want := range []string{`<span class="badge-true">Yes</span>`, `<span class="badge-false">No</span>`} {
		if !strings.Contains(body, want) {
			t.Errorf("changelist lacks the boolean cell %s", want)
		}
	}
}
