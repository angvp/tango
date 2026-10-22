// Package testhook lets i18n/i18ntest reach the package's global state
// without exporting a reset from i18n itself.
package testhook

// Isolate clears the whole i18n state (catalogs, the missing-translation hook
// and its once-state, the observed keys) and returns a function that puts back
// what was there. i18n sets it when it is imported.
var Isolate func() (restore func())
