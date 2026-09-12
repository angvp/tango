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

All of it runs against the same table your migrations created — there's no separate admin-specific schema.
