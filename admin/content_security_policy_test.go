package admin_test

import (
	"fmt"
	"html/template"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// adminPolicyHeader is the policy admin.ContentSecurityPolicy renders to, in
// the package's fixed directive order.
const adminPolicyHeader = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; form-action 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"

// composedAdminOptions attaches the admin policy the way a host does: the
// admin tier wraps SecurityHeaders with the policy value.
func composedAdminOptions() []admin.Option {
	return []admin.Option{admin.WithMiddleware(
		tango.SecurityHeaders(tango.WithContentSecurityPolicy(admin.ContentSecurityPolicy())),
	)}
}

func TestAdminPagesCarryExactlyOneComposedPolicyHeader(t *testing.T) {
	handler, _ := buildProductAdminWithOptions(t, admin.Options{ListDisplay: []string{"Name"}}, composedAdminOptions()...)

	for _, path := range []string{"/admin/login/", crudBasePath} {
		response := doRequest(t, handler, http.MethodGet, path, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, response.Code)
		}
		values := response.Header().Values("Content-Security-Policy")
		if len(values) != 1 || values[0] != adminPolicyHeader {
			t.Errorf("GET %s Content-Security-Policy values = %q, want exactly [%q]", path, values, adminPolicyHeader)
		}
		if got := response.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("GET %s X-Frame-Options = %q, want DENY", path, got)
		}
	}
}

func TestAdminPagesCarryNoPolicyHeaderWithoutComposition(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	response := doRequest(t, handler, http.MethodGet, "/admin/login/", nil)
	if values, present := response.Header()["Content-Security-Policy"]; present {
		t.Fatalf("admin page without composition carried Content-Security-Policy %q", values)
	}
}

// A host logo on another origin shows only when the host adds that origin to
// the composed img-src. The second admin is built from a fresh policy value,
// so it also shows that the first admin's edit did not reach the admin value.
func TestAdminLogoOriginNeedsTheHostToAddItToTheComposedPolicy(t *testing.T) {
	const logo = "https://logo.example.com/mark.png"
	const logoOrigin = "https://logo.example.com"
	branding := admin.WithBranding(admin.Branding{Name: "Acme", LogoURL: logo})

	hostPolicy := admin.ContentSecurityPolicy()
	hostPolicy.ImgSrc = append(hostPolicy.ImgSrc, tango.Host(logoOrigin))
	withHostOrigin, _ := buildProductAdminWithOptions(t, admin.Options{ListDisplay: []string{"Name"}},
		branding, admin.WithMiddleware(tango.SecurityHeaders(tango.WithContentSecurityPolicy(hostPolicy))))
	response := doRequest(t, withHostOrigin, http.MethodGet, crudBasePath, nil)
	if got := response.Header().Get("Content-Security-Policy"); !strings.Contains(got, "img-src 'self' "+logoOrigin) {
		t.Fatalf("composed header = %q, want img-src to name %s", got, logoOrigin)
	}
	if !strings.Contains(response.Body.String(), logo) {
		t.Fatalf("page does not show the logo %s", logo)
	}

	adminOnly, _ := buildProductAdminWithOptions(t, admin.Options{ListDisplay: []string{"Name"}},
		branding, composedAdminOptions()[0])
	response = doRequest(t, adminOnly, http.MethodGet, crudBasePath, nil)
	if got := response.Header().Get("Content-Security-Policy"); got != adminPolicyHeader {
		t.Fatalf("admin-only header = %q, want %q", got, adminPolicyHeader)
	}
	if strings.Contains(response.Header().Get("Content-Security-Policy"), logoOrigin) {
		t.Fatal("the admin policy alone names the logo origin")
	}
}

// inlineStyleWidget is a host widget that emits an inline style attribute. The
// strict admin policy blocks it, which is the documented host-widget contract.
type inlineStyleWidget struct{}

func (inlineStyleWidget) Render(f admin.FieldContext) template.HTML {
	return template.HTML(`<input id="field-` + f.Name + `" name="` + f.Name + `" style="color: red">`)
}

func (inlineStyleWidget) Parse(admin.FieldContext, admin.FieldValues, reflect.Value) error {
	return nil
}

// The contract for host widgets (admin.Widget) under the composed policy: a
// widget that writes an inline style attribute still renders the attribute,
// and the policy's style-src is 'self' alone, so the browser blocks it. The
// test records the contract and does not try to make the widget pass.
func TestHostWidgetInlineStyleIsRenderedAndNotAllowedByTheAdminPolicy(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		Widgets: map[string]admin.Widget{"Name": inlineStyleWidget{}},
	}, composedAdminOptions()...)
	id := seedProduct(t, sqlDB, "Widget", 2.5)

	response := doRequest(t, handler, http.MethodGet, crudBasePath+fmt.Sprint(id)+"/", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET edit = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), `style="color: red"`) {
		t.Fatal("the host widget's inline style attribute is missing from the page")
	}
	if got := response.Header().Get("Content-Security-Policy"); !strings.Contains(got, "style-src 'self';") {
		t.Fatalf("composed header = %q, want style-src 'self' with no inline allowance", got)
	}
}

// The admin tier is inside the global tier, so its policy is the only one on
// an admin response. Outside the admin the global policy still applies, which
// shows the admin header replaced it rather than the global being absent.
func TestAdminTierReplacesTheGlobalPolicyWithOneHeader(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Models().Register(crudProduct{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)

	catalog := tango.NewApp("catalog", func(r *tango.Registry) error {
		if err := r.Models().Register(crudProduct{}); err != nil {
			return err
		}
		if err := r.Admin().Register(crudProduct{}, admin.Options{ListDisplay: []string{"Name"}}); err != nil {
			return err
		}
		return r.Routes().Include("/", tango.URLs{
			tango.Path(http.MethodGet, "/shop/", func(ctx *tango.Context) error {
				return ctx.JSON(http.StatusOK, map[string]string{"ok": "yes"})
			}),
		})
	})
	config := tango.Config{
		InstalledApps:   []tango.App{catalog, admin.New(store, composedAdminOptions()...)},
		Middleware:      []tango.Middleware{tango.SecurityHeaders()},
		MiddlewareScope: tango.MiddlewareScopeAll,
	}
	built, err := tango.BuildRegistry(config)
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	if err := built.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := built.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	shop := doRequest(t, handler, http.MethodGet, "/shop/", nil)
	if got := shop.Header().Get("Content-Security-Policy"); !strings.HasPrefix(got, "default-src 'self'; script-src 'self' 'nonce-") {
		t.Fatalf("global policy on /shop/ = %q, want the global baseline", got)
	}

	login := doRequest(t, handler, http.MethodGet, "/admin/login/", nil)
	values := login.Header().Values("Content-Security-Policy")
	if len(values) != 1 || values[0] != adminPolicyHeader {
		t.Fatalf("admin Content-Security-Policy values = %q, want exactly [%q]", values, adminPolicyHeader)
	}
}
