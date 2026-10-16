package admin_test

import (
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// The admin is pages for browsers, so none of its routes belong in generated
// API documentation.
func TestEveryAdminRouteIsHiddenFromAPIDocumentation(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminShellUser{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Admin().Register(adminShellUser{}, admin.Options{}); err != nil {
		t.Fatal(err)
	}
	_, store := migratedAdminDB(t, registry)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	routes := registry.Routes().Described()
	if len(routes) == 0 {
		t.Fatal("the admin registered no routes")
	}
	for _, route := range routes {
		if route.Doc == nil || !route.Doc.Hidden {
			t.Errorf("%s %s is not hidden from API documentation", route.Method, route.Pattern)
		}
	}
}
