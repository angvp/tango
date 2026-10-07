# Use Tailwind's Play CDN for the Admin Default Theme

## Context

tanGO's admin has a django-grappelli-inspired, shadcn/ui-influenced default theme. tanGO is a pure-Go, server-rendered (`html/template`) framework; admin's templates are Go string literals compiled into the binary, with no Node/npm toolchain anywhere else in the project.

Styling the theme with Tailwind requires deciding how Tailwind's CSS gets produced:

- **Standalone Tailwind CLI, compiled ahead of time**: a documented dev-time step compiles a stylesheet from a small input file using Tailwind's dependency-free standalone binary (not npm). The compiled CSS is committed to the repo and embedded via `embed.FS`, so building tanGO or running admin never requires Tailwind installed, and admin pages load a small static file with no external requests.
- **Tailwind's Play CDN `<script>` tag**: the browser loads Tailwind's CDN script, which JIT-compiles utility classes at runtime from a `<script>`-tag config and a `<style type="text/tailwindcss">` block (which does support `@layer`/`@apply`). Zero build step, but every admin page load ships and runs a JIT compiler in the browser, and requires the admin's user-agent to reach an external CDN.

## Decision

Admin's default theme uses Tailwind's Play CDN script, not a compiled build. This was a deliberate, explicit trade-off: the project preferred zero build tooling over the offline-friendliness and performance of a precompiled stylesheet.

## Consequences

- No Node/npm/Tailwind CLI dependency is introduced anywhere in the tanGO toolchain or CI.
- Admin pages require network access to the CDN and pay a runtime JIT-compilation cost on every load; this is acceptable for a default theme aimed at local development and small deployments, not a hard performance requirement.
- Moving to a compiled build later (for offline use or production performance) is real, non-trivial follow-up work, not a drop-in swap — this ADR should be revisited if that need becomes concrete.
- Component styling is still defined once via Tailwind's `@layer components` inside a `<style type="text/tailwindcss">` block in the shared base layout template, keeping individual page templates free of long inline utility-class lists.
