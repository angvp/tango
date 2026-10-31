package browsersmoke_test

import (
	"context"
	"encoding/json"
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
	ScrollWidth int64         `json:"scrollWidth"`
	ClientWidth int64         `json:"clientWidth"`
	Small       []smallTarget `json:"small"`
	Animations  int           `json:"animations"`
}

type smallTarget struct {
	Tag    string  `json:"tag"`
	Text   string  `json:"text"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// layoutScript measures the page. Inline links inside running prose are
// exempt from the target size rule because WCAG 2.5.8 exempts targets that
// sit inline within a sentence. A control of one pixel or less is clipped
// until it takes focus (the skip link), so it is not a rendered target.
const layoutScript = `(function () {
	var root = document.documentElement;
	var out = {scrollWidth: root.scrollWidth, clientWidth: root.clientWidth, small: [], animations: 0};
	var controls = document.querySelectorAll('a[href], button, input:not([type=hidden]), select, summary, textarea');
	Array.prototype.forEach.call(controls, function (n) {
		var cs = getComputedStyle(n);
		if (cs.display === 'none' || cs.visibility === 'hidden') return;
		var target = n;
		if ((n.type === 'checkbox' || n.type === 'radio') && n.closest('label')) target = n.closest('label');
		var r = target.getBoundingClientRect();
		if (r.width <= 1 && r.height <= 1) return;
		var inProse = n.tagName === 'A' && n.closest('p, li') !== null;
		if (inProse) return;
		if (r.width < 24 || r.height < 24) {
			out.small.push({tag: n.tagName.toLowerCase(), text: (n.innerText || n.value || n.getAttribute('aria-label') || '').trim().slice(0, 40), width: r.width, height: r.height});
		}
	});
	out.animations = document.getAnimations().filter(function (a) {
		return a.playState === 'running' && a.effect && a.effect.getComputedTiming().duration > 5;
	}).length;
	return JSON.stringify(out);
})()`

// TestAdminLayoutAtPhoneWidths measures the admin pages at the two phone
// widths the admin is designed for. It asserts that no page scrolls
// horizontally, that every visible control meets the 24 CSS pixel target,
// and that the drawer opens with no running animation under reduced motion.
func TestAdminLayoutAtPhoneWidths(t *testing.T) {
	binary := chromeBinary(t)
	if binary == "" {
		if os.Getenv("TANGO_REQUIRE_BROWSER") == "1" {
			t.Fatal("TANGO_REQUIRE_BROWSER=1 but no Chrome or Chromium was found; install one or set TANGO_BROWSER_PATH")
		}
		t.Skip("no Chrome or Chromium found locally; the layout measurement did not run")
	}

	server := httptest.NewServer(composedSite(t))
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(binary), chromedp.WindowSize(mobileWidth, mobileHeight))...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocator)
	t.Cleanup(func() {
		cancelBrowser()
		cancelAllocator()
		server.Close()
	})

	if _, err := chromedp.Run(browserCtx, chromedp.Navigate("about:blank")); err != nil {
		t.Fatalf("start the browser: %v", err)
	}
	s := &smoke{t: t, ctx: browserCtx, base: server.URL, rec: &recorder{origin: server.URL}}
	s.rec.begin("setup")

	s.visit("admin login", "/admin/login/")
	s.run("admin login submit",
		chromedp.SendKeys("#field-username", smokeAdminUser),
		chromedp.SendKeys("#field-password", smokeAdminPassword),
		chromedp.Submit("#field-password"),
		chromedp.WaitVisible("#sidebar"),
	)

	pages := []struct{ label, path string }{
		{"dashboard", "/admin/"},
		{"changelist", "/admin/smoke_note/"},
		{"create form", "/admin/smoke_code/new/"},
		{"delete confirm", "/admin/smoke_note/1/delete/"},
		{"login", "/admin/login/"},
		{"404", "/admin/no-such-page/"},
	}
	widths := []struct {
		name          string
		width, height int
	}{
		{"390x844", 390, 844},
		{"320x640", 320, 640},
	}
	for _, w := range widths {
		s.setWidth(w.width, w.height)
		for _, p := range pages {
			s.visit(w.name+" "+p.label, p.path)
			s.checkLayout(w.name+" "+p.label, s.layout())
		}
	}

	s.run("reduced motion", chromedp.Func(func(c context.Context, _ *chromedp.Target) error {
		_, err := chromedp.Call(c, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
			Features: []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}},
		})
		return err
	}))
	s.visit("drawer reduced motion", "/admin/", chromedp.Click(".menu-btn", chromedp.NodeVisible))
	s.check(s.evalInt(`document.getAnimations().filter(function (a) {
		return a.playState === 'running' && a.effect && a.effect.getComputedTiming().duration > 5;
	}).length`) == 0, "the drawer opened with a running animation under prefers-reduced-motion: reduce")

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

// checkLayout records each way one page fails the layout rules.
func (s *smoke) checkLayout(page string, r layoutReport) {
	s.t.Helper()
	s.check(r.ScrollWidth <= r.ClientWidth, "%s scrolls horizontally: scrollWidth %d > clientWidth %d", page, r.ScrollWidth, r.ClientWidth)
	for _, t := range r.Small {
		s.check(false, "%s: control <%s> %q is %.1fx%.1f CSS px, below %d", page, t.Tag, strings.TrimSpace(t.Text), t.Width, t.Height, minTarget)
	}
}

// evalInt evaluates expr and returns its numeric value.
func (s *smoke) evalInt(expr string) int64 {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(s.ctx, stepTimeout)
	defer cancel()
	n, err := chromedp.Run(ctx, chromedp.Evaluate[int64](expr))
	if err != nil {
		s.t.Fatalf("evaluate %q: %v", expr, err)
	}
	return n
}
