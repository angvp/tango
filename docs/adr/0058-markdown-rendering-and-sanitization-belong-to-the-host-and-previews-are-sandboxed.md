# Markdown rendering and sanitization belong to the host, and the admin preview is a sandboxed document

tanGO ships no Markdown parser, sanitizer or rich-text dependency. The Markdown widget stores the author's text exactly as submitted, and the admin never renders the stored text itself. Whether HTML is rendered, and under what sanitization policy, is the host's decision for every place the text is shown publicly. A host supplies that rendering through `admin.WithMarkdownPreview`, and the same callback serves the admin preview.

The admin preview is a different trust context from a public page. Its author is staff, but the HTML is produced from the author's text by code the host wrote, and it must not run in the admin origin. So the preview endpoint returns JSON only, `{"html": ...}`, and never HTML. Admin wraps a successful render in a minimal document that carries a CSP meta, `default-src 'none'; base-uri 'none'; form-action 'none'`, and the client assigns that document to `srcdoc` of an iframe with `sandbox=""` and `referrerpolicy="no-referrer"`. An empty sandbox token means no script, an opaque origin with no admin cookies or storage, no form submission, no popups and no top-level navigation. The CSP does not depend on the sandbox and is kept as defence in depth. Its `default-src 'none'` also blocks images, fonts, styles and frames inside the preview, and that is deliberate: a preview shows the text's structure, not its outbound resources. The policy allows nothing else, so a host's inline styles are dropped too; the document needs none of its own.

The endpoint never persists anything, is POST-only, and is staff-only and CSRF-checked through the same mechanisms as the admin's forms. Its responses are `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.

Rejected:

- **Shipping a Markdown parser or sanitizer.** It would add a dependency and a security policy the framework cannot know is right for a host, and would imply that administrator-authored Markdown is safe to publish.
- **Rendering the preview inline in the admin page.** It would run host-produced HTML in the admin origin and require an admin-wide CSP, which M50 does not add.
- **A sandbox with `allow-scripts` or `allow-same-origin`.** Either lets the preview act as the admin origin.
- **Rendering Markdown in the browser.** It needs a parser in the admin's assets and a second rendering path that can disagree with the host's.
- **Treating the admin preview as public output.** The public page still needs the host's own sanitization policy, whatever the preview shows.

Consequences: a host writes and tests its own renderer, and sanitizes it for public pages. The guide's example renders with Goldmark's defaults, which escape raw HTML, and says so. Previews show no images and no styles, and a renderer that ignores its context keeps running after the deadline, because the deadline is cooperative. The 1 MiB limit applies to the HTML the host returns; the JSON encoding of that HTML can be several times larger, which is still bounded by the same limit.
