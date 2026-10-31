package admin_test

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

func changelistWith(t *testing.T, cookies ...*http.Cookie) string {
	t.Helper()
	handler, sqlDB := buildProductAdmin(t)
	seedProduct(t, sqlDB, "Notes on Type", 12.5)
	all := append([]*http.Cookie{{Name: "tango_admin_session", Value: testSessionToken}}, cookies...)
	return getWithCookies(t, handler, crudBasePath, all...).Body.String()
}

func TestChangelistDensityComesFromTheCookie(t *testing.T) {
	tests := []struct {
		name   string
		cookie *http.Cookie
		want   string
	}{
		{name: "no cookie is comfortable", want: "table-comfortable"},
		{name: "compact cookie is compact", cookie: &http.Cookie{Name: "tango_admin_density", Value: "compact"}, want: "table-compact"},
		{name: "invalid cookie falls back to comfortable", cookie: &http.Cookie{Name: "tango_admin_density", Value: "tiny"}, want: "table-comfortable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if tt.cookie != nil {
				cookies = append(cookies, tt.cookie)
			}
			body := changelistWith(t, cookies...)
			if !hasClassToken(body, tt.want) {
				t.Errorf("changelist table lacks the %s class", tt.want)
			}
		})
	}
}

func TestChangelistRowActionsNameTheirRow(t *testing.T) {
	body := changelistWith(t)
	for _, want := range []string{`aria-label="Edit Notes on Type"`, `aria-label="Delete Notes on Type"`} {
		if !regexp.MustCompile(`<a [^>]*` + regexp.QuoteMeta(want) + `[^>]*>`).MatchString(body) {
			t.Errorf("changelist has no row action carrying %s", want)
		}
	}
}

func TestChangelistNumericColumnsAreRightAlignedWithTabularFigures(t *testing.T) {
	body := changelistWith(t)
	if !regexp.MustCompile(`<th [^>]*>Price</th>`).MatchString(body) {
		t.Fatal("changelist has no Price header")
	}
	if !hasClassToken(regexp.MustCompile(`<th [^>]*>Price</th>`).FindString(body), "cell-num") {
		t.Error("Price header lacks the cell-num class")
	}
	if !hasClassToken(regexp.MustCompile(`<td [^>]*>(?:<span [^>]*>[^<]*</span>)?12\.5</td>`).FindString(body), "cell-num") {
		t.Error("Price cell lacks the cell-num class")
	}
}

func TestChangelistOffersDensityChoicesWithThePressedStateInText(t *testing.T) {
	for _, tt := range []struct {
		name    string
		cookies []*http.Cookie
		pressed string
	}{
		{name: "default pressed is comfortable", pressed: "comfortable"},
		{name: "compact cookie presses compact", cookies: []*http.Cookie{{Name: "tango_admin_density", Value: "compact"}}, pressed: "compact"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := changelistWith(t, tt.cookies...)
			if !strings.Contains(body, `action="/admin/density/"`) {
				t.Fatal("changelist has no density form posting to /admin/density/")
			}
			for _, density := range []string{"comfortable", "compact"} {
				pressed := map[bool]string{true: "true", false: "false"}[density == tt.pressed]
				button := regexp.MustCompile(`<button [^>]*name="density" value="` + density + `"[^>]*>`).FindString(body)
				if button == "" {
					t.Fatalf("no density button for %s", density)
				}
				if !strings.Contains(button, `aria-pressed="`+pressed+`"`) {
					t.Errorf("density button %s is not aria-pressed=%s: %s", density, pressed, button)
				}
				state := map[bool]string{true: "density-option-on", false: "density-option-off"}[density == tt.pressed]
				if !hasClassToken(button, state) {
					t.Errorf("density button %s lacks the %s class: %s", density, state, button)
				}
			}
		})
	}
}

func TestChangelistCellsCarryTheirColumnLabelForThePhoneCard(t *testing.T) {
	body := changelistWith(t)
	for _, want := range []string{`<span class="cell-label">Name</span>`, `<span class="cell-label">Price</span>`} {
		if !strings.Contains(body, want) {
			t.Errorf("changelist cell lacks %s", want)
		}
	}
}

func TestChangelistDisplayColumnIsNotNumericOnHeaderOrCell(t *testing.T) {
	handler, sqlDB := buildProductAdminWithOptions(t, admin.Options{
		ListDisplay: []string{"Name", "Price"},
		Display: map[string]admin.DisplayFunc{"Price": func(context.Context, any) admin.Display {
			return admin.Display{Text: "custom-price"}
		}},
	})
	seedProduct(t, sqlDB, "Alpha", 12.5)

	body := doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String()
	header := regexp.MustCompile(`<th [^>]*>Price</th>`).FindString(body)
	if header == "" {
		t.Fatal("changelist has no Price header")
	}
	if hasClassToken(header, "cell-num") {
		t.Errorf("a Display column header is right-aligned: %s", header)
	}
	cell := regexp.MustCompile(`<td[^>]*>(?:<span [^>]*>[^<]*</span>)?custom-price</td>`).FindString(body)
	if cell == "" {
		t.Fatal("the Display cell is missing")
	}
	if hasClassToken(cell, "cell-num") {
		t.Errorf("a Display cell is right-aligned: %s", cell)
	}
}
