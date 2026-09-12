# Guide: admin registration

tanGO's admin is a server-rendered HTML app: list, create, edit, and delete pages for any model you register with it, behind HTTP Basic Auth.

## Registering a model

Admin registration is separate from model registration, and typically lives right next to it in the same app:

```go
if err := registry.Models().Register(Post{}); err != nil {
	return err
}
if err := registry.Admin().Register(Post{}, admin.Options{
	ListDisplay: []string{"Title", "CreatedAt"},
	Search:      []string{"Title"},
	Ordering:    []string{"CreatedAt"},
}); err != nil {
	return err
}
```

`admin.Options` fields all reference **Go field names**, not column names, and are validated against the model's actual fields at registration time — a typo fails `Register` immediately with a clear error, never a silent no-op:

- **`ListDisplay`** — columns shown on the list page.
- **`Search`** — fields a free-text search box filters against.
- **`Ordering`** — fields the list page can be sorted by.

## Adding the admin app

The admin is its own `tango.App`, constructed with a `*db.Store` and Basic Auth credentials:

```go
admin.New(store, admin.Credentials{
	Username: "admin",
	Password: os.Getenv("TANGO_ADMIN_PASSWORD"),
})
```

Add it to `Config.InstalledApps` **after** every app that registers models with `registry.Admin()` — the admin app reads what's already been registered with `registry.Admin()` when its own `Register` runs, so order matters (per [`InstalledApps`'s ordering guarantee](configuration.md)).

## Authentication

There is no separate login page or session/cookie system: admin routes are protected by HTTP Basic Auth directly, so a browser's built-in credential prompt is the entire login flow. This is a v0.1 simplification, not a full auth story — see [limitations](../limitations.md) for the security boundary this implies.

## Routes

Once registered, a model at `/admin/<table>/` gets:

- `GET /admin/<table>/` — list, with pagination, sorting, and search.
- `GET`/`POST /admin/<table>/new/` — create.
- `GET`/`POST /admin/<table>/{pk}/` — edit.
- `GET`/`POST /admin/<table>/{pk}/delete/` — delete confirmation.

`GET /admin/` (and `/admin`, without the trailing slash) redirects to the first registered model, sorted alphabetically by name — there's no separate "admin home" page to build. With nothing registered yet, it renders a minimal empty state instead of 404ing.

All of it runs against the same table your migrations created — there's no separate admin-specific schema.

## Navigation and multiple models

Every model registered with the admin appears in a sidebar, sorted alphabetically, with the current model highlighted — you don't wire this up yourself; `admin.New` builds it once from `registry.Admin().Registrations()` and threads it through every page. Registering a second model is exactly the same call shown above, from a different app if you like:

```go
if err := registry.Models().Register(Author{}); err != nil {
	return err
}
if err := registry.Admin().Register(Author{}, admin.Options{
	ListDisplay: []string{"Name", "Email"},
}); err != nil {
	return err
}
```

Both `Post` and `Author` now show up in the sidebar, and `/admin/` redirects to `/admin/author/` (alphabetically first).

## List page display

List and form field labels are humanized for display — `CreatedAt` renders as "Created At", `UserID` as "User ID" — without you naming anything twice; the underlying field name (used for sorting, search, and form submission) is untouched. The list page also reports a true total row count and a paginator with page-number links, using the same truncated-range presentation Django's admin uses once there are many pages (`1 2 … 7 8 9 10 … 19 20`).
