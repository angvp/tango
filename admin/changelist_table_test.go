package admin_test

import (
	"net/http"
	"regexp"
	"testing"
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
	if !hasClassToken(regexp.MustCompile(`<td [^>]*>12\.5</td>`).FindString(body), "cell-num") {
		t.Error("Price cell lacks the cell-num class")
	}
}
