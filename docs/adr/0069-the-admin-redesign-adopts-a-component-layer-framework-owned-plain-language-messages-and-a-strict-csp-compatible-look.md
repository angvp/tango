# The admin redesign adopts a component layer, framework-owned plain-language messages and a strict-CSP-compatible look

Status: Accepted. Implemented on the unreleased v0.8.0 line, together with the security headers work. Not released.

Superseded in part by [the compatibility contract](../compatibility.md): the Consequences statement that the admin's CSS and class names stay outside the Covered API still holds, except for the semantic colour token utilities that `docs/compatibility.md` lists as covered under Colour. Component class names, raw palette colours, palette values and every token name not listed there stay outside the Covered API. `docs/compatibility.md` is the authoritative contract.

Builds on [0062](0062-admin-theme-is-semantic-colour-tokens-selected-by-system-preference-with-a-cookie-override.md), and revises its colour values.

## Context

The admin worked, but it read as a developer tool. Its primary users are non-technical staff. An audit found five defects: raw Go error text on the page, form values reset after a failed save, a unique-constraint violation that returned HTTP 500, a phone table whose Actions column was off-screen, and JSON error bodies shown in a browser. Outcome and error messages were scattered across templates, and the admin's own error pages returned JSON to browser requests.

The admin must keep its delivery constraints: no inline script, style or event handler, no third-party origin, and assets precompiled and content-hashed. The framework page audit must keep an empty red list, and the browser smoke test must stay green. See the [content security policy guide](../guides/content-security-policy.md).

## Decision

- **Redesign, not refinement.** The admin adopts a new visual identity. The semantic token structure with light and dark blocks from 0062 is kept. The colour values change: navy `#013b8a` is the action colour on light surfaces, and cyan `#01aade` is the action colour on dark surfaces. Ink is `#0b1f3f` on light surfaces. The values are defined in one table in `admin/theme_palette_test.go`, and the contrast tests read that table.
- **Contrast thresholds.** Every declared token pair meets 4.5:1 for normal text and 3:1 for large text and non-text UI, in both themes. The header has its own `header` token, so in the dark theme the header band is distinct from the sidebar.
- **Named component layer.** Repeated class strings move into named classes in `@layer components` of the precompiled stylesheet. The typeface is the system UI stack, so no font file enters the admin's policy.
- **Framework-owned messages.** One internal path turns store, parse and constraint errors into field errors or page messages. A technical failure on save shows a plain message. The full error is logged on the server, on the same line as a request reference. No Go type name, SQL or parser text reaches the page.
- **HTML error pages.** The admin's HTML routes return an HTML page in the shared layout for 403, 404, 405, 429, an expired form and 500. Each page has a title, one sentence and a next step. A server failure shows the first eight characters of the request ID when the host runs `tango.RequestID`. Without it, no reference is shown.
- **Messages are `admin.*` keys with English fallbacks.** A host overrides any wording one key at a time through its own catalog. The keys and their placeholders are Covered API. The English wording is not.
- **Per-page titles.** Each admin page has its own browser title, which starts with the page heading and ends with "tanGO Admin".
- **`admin.Widget` takes a request context.** `Render` and `Parse` receive `context.Context` as their first argument, so a widget can translate its own text. This is a compile-time break for host custom widgets, and it is permitted because the widget surface is best-effort (see [0014](0014-admin-extensibility-ships-best-effort-not-stable.md)).
- **Unique violation becomes a field error.** A unique-constraint violation returns HTTP 422 with a message beside the offending field, and the user's other submitted values are kept. If the column cannot be attributed, the message is a form-level message instead. A foreign-key refusal on save or delete shows a plain message.
- **Outcome messages in a session-keyed cookie.** Outcome messages travel in a cookie that carries a message key and its arguments. The cookie is authenticated with an HMAC whose key is derived from the admin session token, so it is bound to the session that set it and cannot be forged. It is set before the redirect. The first signed-in GET that renders it expires it, and so does Dismiss. The server also stops accepting a copy 300 seconds after it was set. **This is not single-use:** within those 300 seconds, a copy of the cookie replayed in the same session is accepted again.
- **Display nouns.** `admin.Options` gains `SingularName` and `PluralName`. Unset, the humanized Go type name is used. Both are plain strings, so **they are not translatable**. This is a known limit.
- **Required marker deferred.** No field is marked Required. The model package has no required or blank metadata, so the admin keeps accepting blank values as before. A Required marker waits until the model package gains that metadata.
- **No status-dot aliases.** The status-dot classes introduced during the milestone have no aliases, because they were never released.

## Supersedes

- The admin palette values of [0062](0062-admin-theme-is-semantic-colour-tokens-selected-by-system-preference-with-a-cookie-override.md), in part. The semantic-token structure and the system-preference-plus-cookie selection stay. The colour values and the contrast tests change with the new roles.
- The Play CDN delivery of [0003](0003-tailwind-play-cdn-for-admin-theme.md), in part. The admin ships a precompiled stylesheet in place of the CDN script.

## Deferred

- A searchable combobox for foreign keys, until a list is too long to scan.
- A command palette and keyboard shortcuts.
- A public error-hook API on `admin.Options`. None is added.
- A column chooser.
- A Required marker on form fields, until the model package has required or blank metadata.
- Translatable display nouns. They remain plain strings.

## Consequences

- The admin's HTML, CSS, class names, asset names, URLs, filter query keys, bulk route and field names, dialog markup, and theme endpoint and cookie stay outside the Covered API. They may change in a minor release with a changelog entry.
- The changes that reach host code are the `admin.Widget` signature, the `SingularName` and `PluralName` fields (additive) and the `admin.*` message keys, which become Covered.
- The visual identity changes for every existing admin deployment on upgrade. This is a deliberate change of look, not of behaviour.
- The unique-violation fix changes a 500 into a 422 with a field message. A host that relied on the 500 sees different behaviour.
- The compiled stylesheet remains a committed build artefact. The CI job `admin-css` fails on drift.
- Strict-CSP compatibility is tested, not assumed: the framework page audit and the Chromium browser smoke test cover it.
- Visual checks in both themes at desktop and phone sizes, and Firefox and Safari checks, are manual release checks. They are not covered by automated tests.
- Message style is held by [`admin/MESSAGE_STYLE.md`](../../admin/MESSAGE_STYLE.md), enforced by review and a bad-input behaviour test, not by a lint.
- The accessibility target is WCAG 2.2 AA for each new admin surface.
