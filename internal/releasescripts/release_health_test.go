package releasescripts

import (
	"strings"
	"testing"
)

const (
	published = "2026-11-02T10:00:00Z"
	// "now" as epoch seconds, after the release above (2026-11-02T10:00:00Z
	// is 1793613600); TestEpochs pins these to the ages they name.
	tenMinutesLater = "1793614200"
	twoHoursLater   = "1793620800"
)

func healthEnv(now string) []string {
	return []string{"NOW=" + now, "SITE_URL=https://site.example"}
}

func homepage(version string) string {
	return `<a href="https://github.com/angvp/tango/releases/tag/` + version + `">Docs for ` + version + `</a>`
}

func TestEpochs(t *testing.T) {
	// Pin the constants above to the instants they name.
	r := runScript(t, "release-health.sh", scenario{
		extra: map[string]string{"release": "v0.3.0 " + published + "\n", "site": homepage("v0.3.0")},
	}, healthEnv(twoHoursLater), "--print-age")
	if !strings.Contains(r.out, "age=7200") {
		t.Fatalf("two hours after %s the age should be 7200s:\n%s", published, r.out)
	}
	r = runScript(t, "release-health.sh", scenario{
		extra: map[string]string{"release": "v0.3.0 " + published + "\n", "site": homepage("v0.3.0")},
	}, healthEnv(tenMinutesLater), "--print-age")
	if !strings.Contains(r.out, "age=600") {
		t.Fatalf("ten minutes after %s the age should be 600s:\n%s", published, r.out)
	}
}

func TestReleaseHealthPassesWhenTheSiteShowsTheLatestRelease(t *testing.T) {
	r := runScript(t, "release-health.sh", scenario{
		extra: map[string]string{"release": "v0.3.0 " + published + "\n", "site": homepage("v0.3.0")},
	}, healthEnv(twoHoursLater))
	if r.exit != 0 {
		t.Fatalf("exit = %d:\n%s", r.exit, r.out)
	}
	for _, path := range []string{"https://site.example/", "https://site.example/docs/", "https://site.example/examples/"} {
		if !strings.Contains(r.calls, "curl "+path) {
			t.Fatalf("the check did not request %s:\n%s", path, r.calls)
		}
	}
	if !strings.Contains(r.calls, "workflow list") {
		t.Fatalf("the token was not checked:\n%s", r.calls)
	}
}

func TestReleaseHealthGivesANewReleaseAnHourToReachTheSite(t *testing.T) {
	sc := scenario{extra: map[string]string{"release": "v0.3.0 " + published + "\n", "site": homepage("v0.2.0")}}
	if r := runScript(t, "release-health.sh", sc, healthEnv(tenMinutesLater)); r.exit != 0 {
		t.Fatalf("ten minutes after a release the old site is expected; exit = %d:\n%s", r.exit, r.out)
	}
	r := runScript(t, "release-health.sh", sc, healthEnv(twoHoursLater))
	if r.exit == 0 {
		t.Fatalf("two hours after a release the old site must fail:\n%s", r.out)
	}
	for _, want := range []string{"v0.3.0", "v0.2.0"} {
		if !strings.Contains(r.out, want) {
			t.Fatalf("the failure does not name %s:\n%s", want, r.out)
		}
	}
}

func TestReleaseHealthFailsWhenAPageDoesNotAnswer(t *testing.T) {
	r := runScript(t, "release-health.sh", scenario{
		extra: map[string]string{"release": "v0.3.0 " + published + "\n", "site": homepage("v0.3.0"), "bad_paths": "/docs/\n"},
	}, healthEnv(twoHoursLater))
	if r.exit == 0 || !strings.Contains(r.out, "/docs/") {
		t.Fatalf("exit=%d, want a failure naming /docs/:\n%s", r.exit, r.out)
	}
}

func TestReleaseHealthFailsWhenTheTokenIsRejectedWithoutPrintingIt(t *testing.T) {
	r := runScript(t, "release-health.sh", scenario{
		extra: map[string]string{"release": "v0.3.0 " + published + "\n", "site": homepage("v0.3.0"), "token_rejected": ""},
	}, append(healthEnv(twoHoursLater), "WEB_TOKEN=ghp_super-secret-token"))
	if r.exit == 0 || !strings.Contains(r.out, "dispatch token") {
		t.Fatalf("exit=%d, want a failure about the dispatch token:\n%s", r.exit, r.out)
	}
	for _, secret := range []string{"ghp_super-secret-token", "token-secret-value", "expire"} {
		if strings.Contains(r.out, secret) {
			t.Fatalf("the output leaks %q:\n%s", secret, r.out)
		}
	}
}

func TestReleaseHealthReportsEveryProblemAtOnce(t *testing.T) {
	r := runScript(t, "release-health.sh", scenario{
		extra: map[string]string{"release": "v0.3.0 " + published + "\n", "site": homepage("v0.2.0"), "token_rejected": "", "bad_paths": "/examples/\n"},
	}, healthEnv(twoHoursLater))
	for _, want := range []string{"v0.2.0", "/examples/", "dispatch token"} {
		if !strings.Contains(r.out, want) {
			t.Fatalf("output does not mention %q:\n%s", want, r.out)
		}
	}
}
