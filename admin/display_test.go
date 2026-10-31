package admin_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// summaryDisplay renders a synthetic Summary column from a whole row, which
// is the shape a host uses to combine several fields into one cell.
func summaryDisplay(_ context.Context, row any) admin.Display {
	product, ok := row.(*crudProduct)
	if !ok {
		return admin.Display{Text: "wrong row type"}
	}
	return admin.Display{Text: product.Name + " at " + strconv.FormatFloat(product.Price, 'f', -1, 64)}
}

func TestListViewRendersSyntheticDisplayColumnWithLabel(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Summary"},
		Display:     map[string]admin.DisplayFunc{"Summary": summaryDisplay},
		Labels:      map[string]string{"Summary": "Product summary"},
	})
	seedProduct(t, sqlDB, "Widget", 9.5)

	response := doRequest(t, handler, http.MethodGet, crudBasePath, nil)
	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !strings.Contains(body, `<th scope="col">Product summary</th>`) {
		t.Fatalf("header for the synthetic column is missing:\n%s", body)
	}
	if !strings.Contains(body, "Widget at 9.5") {
		t.Fatalf("Display text for the row is missing:\n%s", body)
	}
}

func TestListViewDisplayReceivesThePointerToEachRow(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Summary"},
		Display:     map[string]admin.DisplayFunc{"Summary": summaryDisplay},
		Labels:      map[string]string{"Summary": "Summary"},
	})
	seedProduct(t, sqlDB, "Alpha", 1)

	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	if strings.Contains(body, "wrong row type") {
		t.Fatalf("Display did not receive *crudProduct:\n%s", body)
	}
}

func TestListViewDisplayTextIsHTMLEscaped(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Summary"},
		Display: map[string]admin.DisplayFunc{"Summary": func(context.Context, any) admin.Display {
			return admin.Display{Text: `<img src=x onerror="alert(1)">`}
		}},
		Labels: map[string]string{"Summary": "Summary"},
	})
	seedProduct(t, sqlDB, "Alpha", 1)

	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	if strings.Contains(body, `<img src=x`) {
		t.Fatalf("Display text was not escaped:\n%s", body)
	}
	if !strings.Contains(body, "&lt;img src=x") {
		t.Fatalf("escaped Display text is missing:\n%s", body)
	}
}

func TestListViewEmptyDisplayGivesAnEmptyCell(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Summary"},
		Display: map[string]admin.DisplayFunc{"Summary": func(context.Context, any) admin.Display {
			return admin.Display{}
		}},
		Labels: map[string]string{"Summary": "Summary"},
	})
	seedProduct(t, sqlDB, "Alpha", 1)

	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	if !strings.Contains(body, `<span class="cell-label">Summary</span></td>`) {
		t.Fatalf("empty Display should render an empty cell:\n%s", body)
	}
}

func TestListViewDisplayOverridesARegisteredFieldsValue(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		Display: map[string]admin.DisplayFunc{"Price": func(context.Context, any) admin.Display {
			return admin.Display{Text: "custom price"}
		}},
	})
	seedProduct(t, sqlDB, "Alpha", 12.5)

	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	if !strings.Contains(body, "custom price") || strings.Contains(body, "12.5") {
		t.Fatalf("Display should replace the registered field's value:\n%s", body)
	}
}

func TestListViewDisplayHrefRendersALinkWithSafeRel(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Summary"},
		Display: map[string]admin.DisplayFunc{"Summary": func(context.Context, any) admin.Display {
			return admin.Display{Text: "Download", Href: "/documents/7/download/"}
		}},
		Labels: map[string]string{"Summary": "File"},
	})
	seedProduct(t, sqlDB, "Alpha", 1)

	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	want := `<a href="/documents/7/download/" rel="noopener noreferrer">Download</a>`
	if !strings.Contains(body, want) {
		t.Fatalf("missing link %s in:\n%s", want, body)
	}
}

func TestListViewUnsafeDisplayHrefFallsBackToEscapedText(t *testing.T) {
	for _, href := range []string{"javascript:alert(1)", "//evil.example/x", "data:text/html,hi"} {
		t.Run(href, func(t *testing.T) {
			handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
				ListDisplay: []string{"Name", "Summary"},
				Display: map[string]admin.DisplayFunc{"Summary": func(context.Context, any) admin.Display {
					return admin.Display{Text: "<b>click</b>", Href: href}
				}},
				Labels: map[string]string{"Summary": "Summary"},
			})
			seedProduct(t, sqlDB, "Alpha", 1)

			body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
			if strings.Contains(body, `href="`+href) || strings.Contains(body, "<b>click</b>") {
				t.Fatalf("unsafe href %q was rendered:\n%s", href, body)
			}
			if !strings.Contains(body, "&lt;b&gt;click&lt;/b&gt;") {
				t.Fatalf("expected escaped plain text for unsafe href:\n%s", body)
			}
		})
	}
}

// TestListViewDisplayPanicIsNotRecoveredByAdmin pins D13: a panicking host
// Display func is a host bug, so admin lets the panic reach the caller
// instead of swallowing it.
func TestListViewDisplayPanicIsNotRecoveredByAdmin(t *testing.T) {
	type hostBug struct{}
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Summary"},
		Display: map[string]admin.DisplayFunc{"Summary": func(context.Context, any) admin.Display {
			panic(hostBug{})
		}},
		Labels: map[string]string{"Summary": "Summary"},
	})
	seedProduct(t, sqlDB, "Alpha", 1)

	defer func() {
		if recovered := recover(); recovered != (hostBug{}) {
			t.Fatalf("recovered = %v, want the host's panic to reach the caller", recovered)
		}
	}()
	doRequest(t, handler, http.MethodGet, crudBasePath, nil)
	t.Fatal("admin swallowed the Display panic")
}

// TestListViewDisplayPanicUsesTheFrameworkRecoverer pins the other half of
// D13: behind the framework's Recoverer, the same panic becomes a 500
// instead of crashing the server.
func TestListViewDisplayPanicUsesTheFrameworkRecoverer(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Summary"},
		Display: map[string]admin.DisplayFunc{"Summary": func(context.Context, any) admin.Display {
			panic("host bug")
		}},
		Labels: map[string]string{"Summary": "Summary"},
	}, admin.WithMiddleware(tango.Recoverer()))
	seedProduct(t, sqlDB, "Alpha", 1)

	response := doRequest(t, handler, http.MethodGet, crudBasePath, nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 from the framework Recoverer", response.Code)
	}
}

func TestLabelsOverrideARegisteredFieldHeaderOnlyWhenSet(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		Labels:      map[string]string{"Name": "Product name"},
	})
	seedProduct(t, sqlDB, "Alpha", 1)

	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	if !strings.Contains(body, `<th scope="col">Product name</th>`) {
		t.Fatalf("Labels did not override the Name header:\n%s", body)
	}
	if !strings.Contains(body, `<th scope="col" class="cell-num">Price</th>`) {
		t.Fatalf("a field without a Labels entry lost its default header:\n%s", body)
	}
}
