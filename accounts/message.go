package accounts

import (
	"context"
	"fmt"

	"github.com/angvp/tango/i18n"
)

// message is user-facing text with its translation key: the HTML pages show
// it through i18n.T, while the JSON API shows the English text unchanged
// (clients branch on the error code).
type message struct {
	key      string
	fallback string
	args     []any
}

// set reports whether m carries a message at all.
func (m message) set() bool { return m.key != "" }

// english is the untranslated text.
func (m message) english() string {
	if len(m.args) == 0 {
		return m.fallback
	}
	return fmt.Sprintf(m.fallback, m.args...)
}

// in is the text for the locale ctx carries.
func (m message) in(ctx context.Context) string {
	return i18n.T(ctx, m.key, m.fallback, m.args...)
}
