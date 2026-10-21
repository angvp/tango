package observability_test

import (
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/observability"
)

// Distinct, recognisable values for every identifying dimension a Window must
// never keep. If any of them reaches a snapshot, its text shows up below.
const (
	canaryRoute  = "/rooms/canary-route-7f3a"
	canaryMethod = "CANARYMETHOD"
	canaryJob    = "canary-job-name-9b2e"
	canaryUser   = "canary-user-id-4c1d"
	canaryRoom   = "canary-room-id-8e5f"
)

var timeType = reflect.TypeOf(time.Time{})

// snapshotLeaves lists every stored field of a Snapshot as "Path Type".
func snapshotLeaves(t reflect.Type, prefix string) []string {
	var out []string
	for i := range t.NumField() {
		f := t.Field(i)
		name := prefix + f.Name
		if f.Type.Kind() == reflect.Struct && f.Type != timeType {
			out = append(out, snapshotLeaves(f.Type, name+".")...)
			continue
		}
		out = append(out, name+" "+f.Type.String())
	}
	return out
}

// TestSnapshotHasNoFieldForIdentifyingValues fixes the complete set of stored
// fields. A route, method, job name, user or room id needs a new field here,
// and that change must be deliberate.
func TestSnapshotHasNoFieldForIdentifyingValues(t *testing.T) {
	want := []string{
		"Scope string",
		"Since time.Time",
		"Window time.Duration",
		"Requests.Total int64",
		"Requests.Class2xx int64",
		"Requests.Class3xx int64",
		"Requests.Class4xx int64",
		"Requests.Class5xx int64",
		"Requests.LatencyP50 float64",
		"Requests.LatencyP95 float64",
		"Requests.LatencyP99 float64",
		"Jobs.Success int64",
		"Jobs.Error int64",
		"Jobs.Panic int64",
		"Jobs.Canceled int64",
	}
	got := snapshotLeaves(reflect.TypeOf(observability.Snapshot{}), "")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Snapshot fields = %q, want %q", got, want)
	}
}

func TestWindowRetainsNoRouteMethodStatusJobUserOrRoom(t *testing.T) {
	clock := &fakeClock{now: windowStart}
	w := newClockedWindow(clock)

	for i := range 50 {
		w.ObserveHistogram(observability.MetricHTTPRequestDuration, 0.3,
			slog.String("route", fmt.Sprintf("%s/%d", canaryRoute, i)),
			slog.String("method", canaryMethod),
			slog.Int("status", 418+i),
			slog.String("user_id", canaryUser),
			slog.String("room", canaryRoom),
		)
	}
	w.AddCounter(observability.MetricSchedulerJobInvocations, 1,
		slog.String("job", canaryJob),
		slog.String("outcome", "panic"),
		slog.String("user_id", canaryUser),
	)
	w.AddCounter(observability.MetricRealtimeRoomEvents, 1,
		slog.String("room", canaryRoom),
		slog.String("outcome", "success"),
	)

	snap := mustSnapshot(t, w)
	if snap.Requests.Total != 50 || snap.Requests.Class4xx != 50 {
		t.Fatalf("Requests = %+v, want 50 recorded 4xx responses", snap.Requests)
	}
	if snap.Jobs.Panic != 1 || snap.Jobs.Success != 0 {
		t.Fatalf("Jobs = %+v, want one panic and no success (realtime ignored)", snap.Jobs)
	}

	text := fmt.Sprintf("%#v\n%+v", snap, snap)
	for _, canary := range []string{canaryRoute, canaryMethod, canaryJob, canaryUser, canaryRoom} {
		if strings.Contains(text, canary) {
			t.Errorf("snapshot text contains %q; identifying values must not be retained", canary)
		}
	}
}
