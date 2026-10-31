package admin_test

import (
	"net/http"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// A host route under the admin's prefix that uses a method the admin answers
// with its HTML 405 page must still reach the host's handler.
func TestHostRoutesUnderTheAdminPrefixReceiveTheMethodsTheAdminAnswers(t *testing.T) {
	const hostStatus = http.StatusTeapot
	hostView := func(ctx *tango.Context) error {
		ctx.ResponseWriter().WriteHeader(hostStatus)
		return nil
	}

	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminTally{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(adminTally{}, admin.Options{SingularName: "Tally"}); err != nil {
		t.Fatalf("register admin model: %v", err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	registry.Routes().Include("/admin/reports/", tango.URLs{
		tango.Path(http.MethodPut, "/{id}/", hostView),
		tango.Path(http.MethodDelete, "/{id}/", hostView),
	})
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		response := doRequest(t, handler, method, "/admin/reports/1/", nil)
		if response.Code != hostStatus {
			t.Errorf("%s /admin/reports/1/ = %d, want the host handler's %d", method, response.Code, hostStatus)
		}
	}
	if response := doRequest(t, handler, http.MethodPut, tallyBasePath+"1/", nil); response.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT on an admin model page = %d, want the admin's 405", response.Code)
	}
}
