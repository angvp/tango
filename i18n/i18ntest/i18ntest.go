// Package i18ntest is test support for code that uses the i18n package.
package i18ntest

import (
	"testing"

	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/i18n/internal/testhook"
)

// Isolate gives the calling test its own empty i18n state and puts the
// previous state back when the test ends. It isolates the registered catalogs,
// the i18n.OnMissing hook together with its record of what was already
// reported, and the keys observed for i18n.Keys and i18n.Validate.
//
// The state belongs to the whole process, so a test that calls Isolate must
// not call t.Parallel, and must not run beside a parallel test that uses the
// i18n package.
func Isolate(t testing.TB) {
	t.Helper()
	_ = i18n.Locale // the import makes i18n register its reset hook
	t.Cleanup(testhook.Isolate())
}
