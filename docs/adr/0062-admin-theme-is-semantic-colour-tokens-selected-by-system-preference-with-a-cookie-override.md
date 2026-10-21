# Admin theme is semantic colour tokens, selected by system preference with a cookie override

## Context

The admin's default theme is Tailwind's Play CDN ([ADR 0003](0003-tailwind-play-cdn-for-admin-theme.md)). Its light palette was written directly into class names across the layout, the dashboard, the widgets, the Markdown preview, the JSON widget and the rendering helpers. There was no dark theme and no `prefers-color-scheme` handling. The sidebar was always dark and the content always light. A browser's forced-dark mode inverted the always-dark sidebar into a light one, so the shell looked inconsistent.

A dark theme forces three choices. The first is where colour lives: a `dark:` variant beside each colour class would double every class use and spread the palette across every template. The second is who owns the markup. Admin renders widgets and Markdown output that the host wrote and that admin does not control, so the theme needs a contract those authors can follow. The third is how the page chooses a theme. An inline script would avoid a flash of the wrong theme, but a strict Content-Security-Policy would block it, so selection has to work in CSS first.

## Decision

Admin colours are semantic tokens. A token has a role-based name, such as `surface`, `ink`, `ink-muted`, `line`, `accent-ui` or `danger-bg`, and a light and a dark value. One Go table declares every token as RGB triplets for both themes. That table is the only place a colour is defined, and it generates three things:

- the CSS custom properties, declared on `:root` and redefined for dark mode;
- the Tailwind colour map, which maps each token to `rgb(var(--token) / <alpha-value>)` so that opacity modifiers such as `text-ink-muted/60` keep working;
- the WCAG contrast tests, which read the same declared pairs, so they cannot drift from the CSS.

Templates use token classes such as `bg-surface` and `text-ink-muted`. There are no per-class `dark:` variants, and admin's own markup has no raw colour utilities.

The theme is chosen in two layers:

- With no choice on record, the stylesheet follows `prefers-color-scheme`. This is pure CSS, so the first paint is correct without a script.
- A signed-in staff member can choose Light, Dark or System from the sidebar footer. Only an explicit Light or Dark is stored, in a cookie scoped to `/admin/`, `HttpOnly`, `SameSite=Lax`, one year long, and `Secure` under the forwarded-protocol rule of [ADR 0039](0039-secure-cookies-trust-forwarded-proto.md). Choosing System deletes the cookie, so `system` is never a stored value. When the cookie is present, the page sets `data-theme` on `<html>` and the matching variables apply.

The choice is a form POST to an admin endpoint. It is an admin action, not a public preference endpoint: it sets nothing until normal staff authentication and CSRF validation have passed. It accepts only `light`, `dark` or System, and it redirects to a `next` value that is validated after URL parsing and path normalisation as a same-origin relative path under `/admin/`. Anything else falls back to `/admin/`. It works without JavaScript. The login page has no switch. It honours the cookie, then the system preference.

Native controls follow the active theme through `color-scheme`, so checkboxes, date inputs and scrollbars render in the matching palette. Scrollbar and selection styles are tokens as well.

Three surfaces keep a fixed appearance on purpose:

- The sidebar has its own tokens. In light it matches the former always-dark surface. In dark it is a shade deeper than the content, with a hairline border, so it stays distinct from the content. Because it is dark in both themes, a logo drawn for a dark background works in both, and no second logo option is added.
- The Markdown preview is a white canvas in both themes, inside a framed panel. It is a deliberately light, isolated surface. It is not a promise to match any public site, and the host owns public rendering, its CSS and its dark-mode behaviour.
- Host widgets get no theming hook. The contract is to use the documented token classes. A widget that hard-codes light colours is the host's to fix, which matches the best-effort admin extensibility stated in [ADR 0014](0014-admin-extensibility-ships-best-effort-not-stable.md).

The accounts package renders no styled pages, so the theme does not reach outside `admin`.

## Alternatives considered

- **Per-class `dark:` variants.** Putting a `dark:` modifier on each of the roughly 75 colour class uses would make the theme a property of every template. A palette change would become a sweep across templates and tests, and nothing would declare both themes in one place. Rejected in favour of one table.
- **A separate dark stylesheet.** A second stylesheet, or a second Tailwind configuration swapped in at load, would duplicate every component rule and make the browser load and compile a second file before the theme was right. Rejected.
- **A JavaScript-only toggle.** Setting the theme from a script after load needs an inline script to avoid a flash, which a strict Content-Security-Policy blocks. It also fails without JavaScript and leaves the first paint dependent on script execution. Rejected: the CSS media query covers system preference, and a form POST covers the override.
- **A Tailwind scale override.** Redefining Tailwind's own gray or slate scale per theme would change what existing classes such as `text-slate-600` mean, and it cannot express semantic roles such as the focus ring or the sidebar surface. Rejected.
- **System only, with no override.** It is simpler and follows the browser setting, but an operator whose environment disagrees with the system setting has no way to choose. Rejected.
- **A stored `system` value.** Storing three values makes the cookie a record of a default. Deleting the cookie as the meaning of System keeps "no override" as the single state. Rejected.
- **A themed Markdown preview.** Following the theme would make the preview resemble the host's rendering only by accident, and its content is host-written HTML that admin does not control. Rejected; the preview stays a fixed light, isolated surface.

## Consequences

- The palette table is the one place a colour changes. Changing a value updates the CSS, the Tailwind map and the contrast tests together. The contrast tests check every declared token pair in both themes: at least 4.5:1 for normal text, and at least 3:1 for large text and required UI components such as the focus ring and input borders. They prove the declared pairs. They do not prove every rendered host widget.
- Admin's rendered pages are checked for raw colour classes. Only the token definition file and a short, documented exceptions list may contain them. New admin markup must use token classes.
- Host widgets and `PreviewWidget` output are legible in dark mode only if they use the documented token classes. Admin cannot make widgets that hard-code light colours legible, and the widget contract says so.
- The Markdown preview is a fixed light canvas in both themes. Dark mode in admin does not change how the host renders public Markdown.
- The cookie is scoped to `/admin/`, so public pages never receive it. It stores only `light` or `dark`, and choosing System removes it. Its `Secure` attribute follows [ADR 0039](0039-secure-cookies-trust-forwarded-proto.md), so local HTTP stays usable.
- The theme is a per-browser presentation preference. It is not account data, so no column or model field stores it and it does not follow a user between browsers.
- Without a cookie there is no forced `data-theme`, and the page's first paint follows `prefers-color-scheme` in CSS. There is no inline script, so a strict Content-Security-Policy keeps working.
- The Tailwind Play CDN dependency is unchanged, as [ADR 0003](0003-tailwind-play-cdn-for-admin-theme.md) records. Dark mode adds no request, no script and no build step.
- `color-scheme` follows the active theme, so native form controls and scrollbars match it. Browsers that ignore `color-scheme` keep their default controls, which remain usable.
