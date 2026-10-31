package browsersmoke_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// minTarget is the smallest rendered edge, in CSS pixels, an interactive
// control may have (WCAG 2.5.8, target size minimum).
const minTarget = 24

// layoutReport is what the layout script returns for one page at one width.
type layoutReport struct {
	ScrollWidth int64             `json:"scrollWidth"`
	ClientWidth int64             `json:"clientWidth"`
	HasSkip     bool              `json:"hasSkip"`
	SkipFocused bool              `json:"skipFocused"`
	Controls    []measuredControl `json:"controls"`
}

// measuredControl is one rendered control and its border-box size.
type measuredControl struct {
	Tag    string  `json:"tag"`
	Text   string  `json:"text"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func (c measuredControl) edge() float64 { return math.Min(c.Width, c.Height) }

// auditRow is one line of the audit table, built from the measured report.
type auditRow struct {
	page, viewport string
	scroll, client int64
	smallest       string
	result         string
}

// layoutScript measures every visible control. Links inside a paragraph are
// not measured, because WCAG 2.5.8 exempts a target that sits inline in
// running text. The skip link is clipped until it takes focus, so it is
// focused before its size is read, and focus is then returned to where it was.
const layoutScript = `(function () {
	var root = document.documentElement;
	var previous = document.activeElement;
	var skip = document.querySelector('.skip-link');
	var out = {scrollWidth: root.scrollWidth, clientWidth: root.clientWidth, hasSkip: skip !== null, skipFocused: false, controls: []};
	var nodes = document.querySelectorAll('a[href], button, input:not([type=hidden]), select, summary, textarea');
	Array.prototype.forEach.call(nodes, function (n) {
		if (n.tagName === 'A' && n.closest('p') !== null) return;
		if (n === skip) n.focus();
		if (!n.checkVisibility({visibilityProperty: true})) return;
		var target = n;
		if (n.type === 'checkbox' || n.type === 'radio') {
			var label = n.closest('label') || (n.id ? document.querySelector('label[for="' + n.id + '"]') : null);
			if (label) target = label;
		}
		var r = target.getBoundingClientRect();
		out.controls.push({tag: n.tagName.toLowerCase(), text: (n.innerText || n.value || n.getAttribute('aria-label') || '').trim().slice(0, 40), width: r.width, height: r.height});
	});
	var focused = document.activeElement;
	out.skipFocused = skip !== null && focused === skip;
	if (focused !== previous) {
		if (previous && previous !== document.body) previous.focus(); else focused.blur();
	}
	return JSON.stringify(out);
})()`

// failedCreateSave fills the create form with two values the widget refuses
// and submits it, leaving the field errors and the summary on the page.
func failedCreateSave() []chromedp.Action[chromedp.Void] {
	return []chromedp.Action[chromedp.Void]{
		chromedp.SetValue("#field-Code", "toolong", chromedp.NodeVisible),
		chromedp.SetValue("#field-Label", "toolong"),
		chromedp.Click("form[method=post] button.btn-primary", chromedp.NodeVisible),
		chromedp.WaitVisible("#form-summary"),
		chromedp.WaitReady(".field-error"),
	}
}

// TestAdminLayoutAtPhoneWidths measures the admin pages at the phone widths the
// admin is designed for. It fails a page that scrolls sideways, a control below
// minTarget, a skip link that was not measured while focused, and a drawer
// that keeps a non-zero transition under reduced motion. It logs the measured
// values of every page as an audit table.
func TestAdminLayoutAtPhoneWidths(t *testing.T) {
	binary := chromeBinary(t)
	if binary == "" {
		if os.Getenv("TANGO_REQUIRE_BROWSER") == "1" {
			t.Fatal("TANGO_REQUIRE_BROWSER=1 but no Chrome or Chromium was found; install one or set TANGO_BROWSER_PATH")
		}
		t.Skip("no Chrome or Chromium found locally; the layout measurement did not run")
	}

	server := httptest.NewServer(composedSite(t))
	browserCtx, cancelBrowser := startBrowser(t, binary)
	t.Cleanup(func() {
		cancelBrowser()
		server.Close()
	})

	if _, err := chromedp.Run(browserCtx, chromedp.Navigate("about:blank")); err != nil {
		t.Fatalf("start the browser: %v", err)
	}
	s := &smoke{t: t, ctx: browserCtx, base: server.URL, rec: &recorder{origin: server.URL}}
	s.rec.begin("setup")
	// Headless windows are not focused, so :focus styles do not apply unless
	// focus emulation is on. The skip link is measured in its focused state.
	s.run("focus emulation", chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
		_, err := chromedp.Call(c, emulation.SetFocusEmulationEnabled, emulation.SetFocusEmulationEnabledParams{Enabled: true})
		return err
	}))
	s.adminLogin()

	pages := []struct {
		label, path string
		actions     []chromedp.Action[chromedp.Void]
	}{
		{"dashboard", "/admin/", nil},
		{"changelist", "/admin/smoke_note/", nil},
		{"create form", "/admin/smoke_code/new/", nil},
		{"create form, failed save", "/admin/smoke_code/new/", failedCreateSave()},
		{"change form, JSON widget", "/admin/smoke_note/1/", []chromedp.Action[chromedp.Void]{chromedp.WaitReady(".json-widget")}},
		{"delete confirm", "/admin/smoke_note/1/delete/", nil},
		{"login", "/admin/login/", nil},
		{"404", "/admin/no-such-page/", nil},
	}
	widths := []struct {
		name          string
		width, height int
	}{
		{"390x844", 390, 844},
		{"320x640", 320, 640},
	}

	var rows []auditRow
	for _, w := range widths {
		s.setWidth(w.width, w.height)
		for _, p := range pages {
			s.visit(w.name+" "+p.label, p.path, p.actions...)
			rows = append(rows, s.checkLayout(p.label, w.name, s.layout()))
		}
	}
	logAudit(t, rows)

	s.run("reduced motion", chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
		_, err := chromedp.Call(c, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
			Features: []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}},
		})
		return err
	}))
	s.visit("drawer reduced motion", "/admin/", chromedp.Click(".menu-btn", chromedp.NodeVisible))
	s.expectOpen()
	s.check(s.evalString(`String(getComputedStyle(document.getElementById('sidebar')).transitionDuration.split(',').every(function (d) { return parseFloat(d) === 0; }))`) == "true", "the drawer keeps a non-zero transition under prefers-reduced-motion: reduce")

	s.report()
}

// layout runs the measurement script on the current page.
func (s *smoke) layout() layoutReport {
	s.t.Helper()
	raw := s.evalString(layoutScript)
	var r layoutReport
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		s.t.Fatalf("decode layout report %q: %v", raw, err)
	}
	return r
}

// checkLayout records each way one page fails the layout rules and returns the
// page's audit row. The row's result is derived from the same conditions the
// checks test, so the table and the failures cannot disagree.
func (s *smoke) checkLayout(page, viewport string, r layoutReport) auditRow {
	name := viewport + " " + page
	scrolls := r.ScrollWidth > r.ClientWidth
	s.check(!scrolls, "%s scrolls horizontally: scrollWidth %d > clientWidth %d", name, r.ScrollWidth, r.ClientWidth)
	skipMissed := r.HasSkip && !r.SkipFocused
	s.check(!skipMissed, "%s: the skip link was not focused when measured", name)

	var smallest *measuredControl
	short := 0
	for i := range r.Controls {
		c := &r.Controls[i]
		if smallest == nil || c.edge() < smallest.edge() {
			smallest = c
		}
		if c.edge() < minTarget {
			short++
			s.check(false, "%s: control <%s> %q is %.1fx%.1f CSS px, below %d", name, c.Tag, strings.TrimSpace(c.Text), c.Width, c.Height, minTarget)
		}
	}

	row := auditRow{page: page, viewport: viewport, scroll: r.ScrollWidth, client: r.ClientWidth, smallest: "none", result: "pass"}
	if smallest != nil {
		row.smallest = fmt.Sprintf("%.1fx%.1f", smallest.Width, smallest.Height)
	}
	if scrolls || short > 0 || skipMissed {
		row.result = "fail"
	}
	return row
}

// logAudit logs every row under one header, one fixed column per field.
func logAudit(t *testing.T, rows []auditRow) {
	t.Helper()
	const format = "%-26s | %-8s | %11s | %11s | %-20s | %s"
	lines := []string{fmt.Sprintf(format, "page", "viewport", "scrollWidth", "clientWidth", "smallest control WxH", "result")}
	for _, r := range rows {
		lines = append(lines, fmt.Sprintf(format, r.page, r.viewport,
			fmt.Sprint(r.scroll), fmt.Sprint(r.client), r.smallest, r.result))
	}
	t.Log("\n" + strings.Join(lines, "\n"))
}
