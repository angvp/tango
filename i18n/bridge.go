package i18n

import "context"

// Selected returns the registered spelling of the catalog that the locale
// carried by ctx selects, matching without regard to case or "-" versus "_"
// and falling back to the base language, or "" when no catalog answers to it.
// It is the value to put in a page's lang attribute.
func Selected(ctx context.Context) string {
	return registeredLocale(Locale(ctx))
}
