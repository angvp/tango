// Package i18nbridge lets packages that already import i18n ask which
// registered catalog a request selected, without i18n exporting a helper for
// it.
package i18nbridge

import "context"

// Selected returns the registered spelling of the catalog the locale carried
// by ctx selects, or "" when no catalog matches. i18n sets it when it is
// imported.
var Selected func(ctx context.Context) string
