package browsersmoke_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/angvp/tango"
)

// TestStrictCSPBlocksCrossOriginFormAction serves a form that posts to a second
// origin under the baseline policy the middleware applies with no options. The
// browser must refuse the submission, report it as a form-action violation, and
// never send the request to the target.
func TestStrictCSPBlocksCrossOriginFormAction(t *testing.T) {
	binary := chromeBinary(t)
	if binary == "" {
		if os.Getenv("TANGO_REQUIRE_BROWSER") == "1" {
			t.Fatal("TANGO_REQUIRE_BROWSER=1 but no Chrome or Chromium was found; install one or set TANGO_BROWSER_PATH")
		}
		t.Skip("no Chrome or Chromium found locally; the cross-origin form-action check did not run (set TANGO_BROWSER_PATH, or TANGO_REQUIRE_BROWSER=1 to fail instead)")
	}

	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	site := httptest.NewServer(tango.SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Cross-origin form</title></head><body>`+
			`<form id="cross" method="post" action="%s/collect"><input name="q" value="1"><button id="send" type="submit">Send</button></form>`+
			`</body></html>`, target.URL)
	})))
	t.Cleanup(site.Close)

	browserCtx, cancelBrowser := startBrowser(t, binary)
	rec := &recorder{origin: site.URL}
	var readers sync.WaitGroup
	consume(&readers, rec, chromedp.Events(browserCtx, runtime.ConsoleAPICalled), rec.console)
	t.Cleanup(func() {
		cancelBrowser()
		readers.Wait()
	})

	rec.begin("setup")
	if _, err := chromedp.Call(browserCtx, page.AddScriptToEvaluateOnNewDocument,
		page.AddScriptToEvaluateOnNewDocumentParams{Source: reporterScript}); err != nil {
		t.Fatalf("install the reporter script: %v", err)
	}

	s := &smoke{t: t, ctx: browserCtx, base: site.URL, rec: rec}
	s.visit("cross-origin form", "/", chromedp.Click("#send"))

	rec.mu.Lock()
	findings := slices.Clone(rec.findings)
	rec.mu.Unlock()
	var blocked []string
	for _, f := range findings {
		if f.source == "securitypolicyviolation" && strings.Contains(f.detail, "directive form-action") {
			blocked = append(blocked, f.detail)
		}
	}
	if len(blocked) == 0 {
		t.Errorf("no form-action violation was reported for the cross-origin form; findings=%+v", findings)
	}
	if got := targetRequests.Load(); got != 0 {
		t.Errorf("the cross-origin form target received %d request(s), want 0", got)
	}
}
