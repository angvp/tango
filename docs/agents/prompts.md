# Optional Prompt Starting Points, Not Framework API

These are convenience snippets for external coding agents. They are not tanGO API, not a contract, and not a replacement for the recipes.

## Add a Model, Migration, and Admin Registration

Add a new tanGO model to this project using `docs/agents/models.md`, generate and inspect its migration using `docs/agents/migrations.md`, then register it with admin using `docs/agents/admin-registration.md`. Keep registration explicit through the app's `Register(*tango.Registry)` path and run `docs/agents/checklist.md` before stopping.

## Add a Foreign Key

Add a foreign-key relationship using `docs/agents/relationships.md`. Register both models explicitly, regenerate migrations, check that the FK target is correct, and update admin options so the related object displays with a useful label.

## Add Login to a Page

Use `docs/agents/auth-and-accounts.md` and `docs/agents/middleware.md` to protect a route with a View wrapper. Use `accounts.RequireLogin` for the default accounts app, or `auth.RequireLogin` if the project owns a custom identity model.

## Package a Reusable App

Turn this feature into an importable reusable app following `docs/agents/reusable-apps.md`. Expose `New(...) tango.App`, keep host dependencies explicit, and do not import host packages from the reusable app.
