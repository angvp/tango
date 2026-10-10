package adminregistry

import "context"

// DisplayFunc computes one changelist cell from a model row. The row is
// passed as a pointer to the model value (for example *Upload), so the
// function can read any of its fields, including ones that are not shown
// as columns. A panic in the function is the host's bug: admin does not
// recover it.
type DisplayFunc func(context.Context, any) Display

// Display is what a changelist cell shows. Text is always HTML-escaped.
// Href, when set, makes the cell a link, but only if it is a relative URL
// or an absolute http or https URL; any other value falls back to plain
// text.
type Display struct {
	Text string
	Href string
}
