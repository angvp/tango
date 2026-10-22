package admin

import (
	"context"

	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/adminregistry"
)

// widgetText translates the fixed text a built-in widget renders, using the
// request context the form attached to the field.
type widgetText struct {
	ctx context.Context
}

func newWidgetText(f FieldContext) widgetText {
	return widgetText{ctx: adminregistry.ContextOf(f)}
}

// T is i18n.T for the widget's request.
func (w widgetText) T(key, fallback string, args ...any) string {
	return i18n.T(w.ctx, key, fallback, args...)
}

// Active reports whether a locale was resolved for the request. Widgets hand
// their script-side messages to the browser only then, so a project without
// i18n gets exactly the markup it always did.
func (w widgetText) Active() bool {
	return i18n.Locale(w.ctx) != ""
}
