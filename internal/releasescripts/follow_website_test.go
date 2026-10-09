package releasescripts

import (
	"strings"
	"testing"
)

// Fast polling: the scripts read these from the environment.
var fast = []string{"WAIT_INTERVAL=0", "APPEAR_TRIES=3", "FINISH_TRIES=3"}

func TestFollowWebsiteSucceedsWhenTheFollowRunSucceeds(t *testing.T) {
	r := runScript(t, "follow-website.sh", scenario{
		before: "101\n102\n", after: "103\n101\n102\n",
		views: []string{"in_progress ", "completed success"},
	}, fast, "v0.3.0")
	if r.exit != 0 {
		t.Fatalf("exit = %d, want 0:\n%s", r.exit, r.out)
	}
	if !strings.Contains(r.calls, "workflow run follow-tango-release.yml") || !strings.Contains(r.calls, "version=v0.3.0") {
		t.Fatalf("the follow workflow was not dispatched for v0.3.0:\n%s", r.calls)
	}
	if !strings.Contains(r.calls, "run view 103") {
		t.Fatalf("it did not watch the new run 103, only:\n%s", r.calls)
	}
}

func TestFollowWebsiteFailsWithTheDocumentedMessageWhenTheFollowRunFails(t *testing.T) {
	for _, conclusion := range []string{"failure", "cancelled", "timed_out"} {
		t.Run(conclusion, func(t *testing.T) {
			r := runScript(t, "follow-website.sh", scenario{
				before: "101\n", after: "102\n101\n", views: []string{"completed " + conclusion},
			}, fast, "v0.3.0")
			if r.exit == 0 {
				t.Fatalf("exit = 0, want failure:\n%s", r.out)
			}
			for _, want := range []string{"release published, website not updated", conclusion, "https://example.test/runs/102", "Follow a tanGO release", "v0.3.0"} {
				if !strings.Contains(r.out, want) {
					t.Fatalf("output does not contain %q:\n%s", want, r.out)
				}
			}
		})
	}
}

func TestFollowWebsiteSaysSoWhenTheRunNeverAppears(t *testing.T) {
	r := runScript(t, "follow-website.sh", scenario{before: "101\n", views: []string{"completed success"}}, fast, "v0.3.0")
	if r.exit == 0 || !strings.Contains(r.out, "release published, website not updated") || !strings.Contains(r.out, "never appeared") {
		t.Fatalf("exit=%d, want a failure saying the run never appeared:\n%s", r.exit, r.out)
	}
}

func TestFollowWebsiteSaysSoWhenTheRunNeverFinishes(t *testing.T) {
	r := runScript(t, "follow-website.sh", scenario{before: "101\n", after: "102\n101\n", views: []string{"in_progress "}}, fast, "v0.3.0")
	if r.exit == 0 || !strings.Contains(r.out, "release published, website not updated") || !strings.Contains(r.out, "did not finish") {
		t.Fatalf("exit=%d, want a failure saying the run did not finish:\n%s", r.exit, r.out)
	}
}

func TestFollowWebsiteReportsAFailedDispatch(t *testing.T) {
	r := runScript(t, "follow-website.sh", scenario{before: "101\n", dispatchFail: true, views: []string{"completed success"}}, fast, "v0.3.0")
	if r.exit == 0 || !strings.Contains(r.out, "release published, website not updated") || !strings.Contains(r.out, "could not start") {
		t.Fatalf("exit=%d, want a failure saying the workflow could not be started:\n%s", r.exit, r.out)
	}
}

func TestFollowWebsiteNeedsAVersion(t *testing.T) {
	if r := runScript(t, "follow-website.sh", scenario{before: "\n", views: []string{""}}, fast); r.exit == 0 {
		t.Fatalf("exit = 0 without a version:\n%s", r.out)
	}
}
