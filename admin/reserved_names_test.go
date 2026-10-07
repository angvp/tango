package admin_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/testdb"
)

// User and Order are named after SQL reserved words on purpose: their
// tables are "user" and "order", and Group/Select/User are reserved column
// names. Their admin pages must work on both Test dialects.
type User struct {
	ID    int64 `tango:"pk"`
	Name  string
	Group string
}

type Order struct {
	ID     int64 `tango:"pk"`
	User   int64 `tango:"fk=User"`
	Select string
}

// buildReservedNamesAdmin wires User and Order through admin on the run's
// Test dialect, with every table (admin's own included) built by the
// framework's migration DDL, and a logged-in staff session.
func buildReservedNamesAdmin(t *testing.T) (http.Handler, *db.Store, *tango.Registry) {
	t.Helper()
	ctx := context.Background()
	sqlDB, dialect := testdb.Open(t)
	store := db.NewStore(sqlDB, dialect)

	registry := tango.NewRegistry()
	for _, value := range []any{User{}, Order{}} {
		if err := registry.Models().Register(value); err != nil {
			t.Fatalf("register %T: %v", value, err)
		}
	}
	if err := registry.Admin().Register(User{}, admin.Options{
		Label: "Name", ListDisplay: []string{"Name", "Group"}, Search: []string{"Group"}, Ordering: []string{"-Group"},
	}); err != nil {
		t.Fatalf("register admin User: %v", err)
	}
	if err := registry.Admin().Register(Order{}, admin.Options{ListDisplay: []string{"User", "Select"}}); err != nil {
		t.Fatalf("register admin Order: %v", err)
	}
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	store.UseModels(registry.Models())
	migrationtest.Apply(t, sqlDB, dialect, registry.Models().All())

	if err := admin.CreateAccount(ctx, store, "admin", "secret"); err != nil {
		t.Fatalf("seed admin account: %v", err)
	}
	userMeta, _ := registry.Models().Get("AdminUser")
	var accounts []admin.AdminUser
	if err := store.List(ctx, userMeta, db.Query{}, &accounts); err != nil || len(accounts) != 1 {
		t.Fatalf("load admin account: %v (%d rows)", err, len(accounts))
	}
	sessionMeta, _ := registry.Models().Get("AdminSession")
	session := admin.AdminSession{Token: testSessionToken, UserID: accounts[0].ID, ExpiresAt: time.Now().Add(time.Hour).UTC()}
	if err := store.Create(ctx, sessionMeta, &session); err != nil {
		t.Fatalf("seed admin session: %v", err)
	}

	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, store, registry
}

func TestAdminListsSearchesAndEditsReservedWordModels(t *testing.T) {
	handler, store, registry := buildReservedNamesAdmin(t)
	ctx := context.Background()
	userMeta, _ := registry.Models().Get("User")
	orderMeta, _ := registry.Models().Get("Order")

	ada := User{Name: "Ada", Group: "admins"}
	bob := User{Name: "Bob", Group: "staff"}
	for _, u := range []*User{&ada, &bob} {
		if err := store.Create(ctx, userMeta, u); err != nil {
			t.Fatalf("Create user: %v", err)
		}
	}

	list := doRequest(t, handler, http.MethodGet, "/admin/user/?q=adm", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body:\n%s", list.Code, list.Body.String())
	}
	if body := list.Body.String(); !strings.Contains(body, "Ada") || strings.Contains(body, "Bob") {
		t.Fatalf("search for %q should list Ada only:\n%s", "adm", body)
	}

	edit := doRequest(t, handler, http.MethodPost, "/admin/user/"+itoa(ada.ID)+"/", url.Values{"Name": {"Ada"}, "Group": {"owners"}})
	if edit.Code != http.StatusFound {
		t.Fatalf("edit status = %d, body:\n%s", edit.Code, edit.Body.String())
	}
	var got User
	if err := store.Get(ctx, userMeta, ada.ID, &got); err != nil {
		t.Fatalf("Get edited user: %v", err)
	}
	if got.Group != "owners" {
		t.Fatalf("Group after edit = %q, want owners", got.Group)
	}

	create := doRequest(t, handler, http.MethodPost, "/admin/order/new/", url.Values{"User": {itoa(bob.ID)}, "Select": {"express"}})
	if create.Code != http.StatusFound {
		t.Fatalf("create order status = %d, body:\n%s", create.Code, create.Body.String())
	}
	orders := doRequest(t, handler, http.MethodGet, "/admin/order/", nil)
	if body := orders.Body.String(); orders.Code != http.StatusOK || !strings.Contains(body, "express") || !strings.Contains(body, "Bob") {
		t.Fatalf("order list status = %d, want the new order labelled with Bob:\n%s", orders.Code, body)
	}

	del := doRequest(t, handler, http.MethodPost, "/admin/user/"+itoa(bob.ID)+"/delete/", url.Values{})
	if del.Code != http.StatusFound {
		t.Fatalf("delete status = %d, body:\n%s", del.Code, del.Body.String())
	}
	remaining, err := store.Count(ctx, orderMeta, db.Query{})
	if err != nil {
		t.Fatalf("Count orders: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("orders after deleting their user = %d, want 0 (cascade)", remaining)
	}
}
