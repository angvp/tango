package profiles

import (
	"bytes"
	"context"
	"fmt"
	"html/template"

	"github.com/yuin/goldmark"
)

// previewBio is the host's Bio renderer for the admin preview. It uses
// Goldmark's defaults, which do not pass raw HTML through and drop unsafe
// link destinations, so the sandboxed preview shows only rendered Markdown.
// That is not a sanitization policy for public pages: a host that shows Bio
// on a public page must render and sanitize it under its own policy (see the
// admin registration guide). A new Markdown instance per call keeps the
// renderer free of shared state.
func previewBio(ctx context.Context, source string) (template.HTML, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("preview bio: %w", err)
	}
	var out bytes.Buffer
	if err := goldmark.New().Convert([]byte(source), &out); err != nil {
		return "", fmt.Errorf("render bio markdown: %w", err)
	}
	return template.HTML(out.String()), nil
}
