package i18n

import (
	"context"

	"github.com/angvp/tango/internal/i18nbridge"
)

func init() { i18nbridge.Selected = selectedLocale }

// selectedLocale is the registered spelling of the catalog ctx's locale
// selects, or "" when it selects none.
func selectedLocale(ctx context.Context) string {
	return registeredLocale(Locale(ctx))
}
