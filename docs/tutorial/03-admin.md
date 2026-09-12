# Tutorial, part 3: admin

Continuing from [part 2](02-persistence-migrations.md), this section registers `Post` with tanGO's HTML admin, giving you list, create, edit, and delete pages without writing any HTML yourself.

## Register the model with the admin

Admin registration is separate from model registration, and lives alongside it in `posts.New`'s `Register` function:

```go
// apps/posts/app.go
import "github.com/angvp/tango/admin"

func New(store *db.Store) tango.App {
	return tango.NewApp("posts", func(registry *tango.Registry) error {
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

		meta, _ := registry.Models().Get("Post")
		return registry.Routes().Include("/posts/", tango.URLs{
			tango.Path("GET", "/", listPosts(store, meta), tango.Name("list")),
			tango.Path("GET", "/{id}/", getPost(store, meta), tango.Name("detail")),
			tango.Path("POST", "/", createPost(store, meta), tango.Name("create")),
		})
	})
}
```

`ListDisplay`, `Search`, and `Ordering` all reference Go field names (not column names) and are validated against the model's actual fields at registration time — a typo fails fast with a clear error, not a silent no-op.

## Add the admin app itself

The admin is its own app, constructed with the same `store` and a set of Basic Auth credentials. Add it to `main.go`, **after** `posts.New(store)` in `InstalledApps` — order matters, since the admin app reads what's already been registered with `registry.Admin()` when it runs:

```go
// main.go
import "github.com/angvp/tango/admin"

config := tango.Config{
	InstalledApps: []tango.App{
		posts.New(store),
		admin.New(store, admin.Credentials{
			Username: "admin",
			Password: "change-me", // read this from an environment variable in anything real
		}),
	},
	Addr: ":8000",
}
```

There is no separate admin login page: admin routes are protected by HTTP Basic Auth directly, so a browser's built-in credential prompt is the entire login flow.

## Run it

```sh
go run .
```

Visit `http://localhost:8000/admin/post/` and authenticate with the credentials above. You get:

- A **list** page with your configured columns, sorting, pagination, and a search box.
- A **create** page (a plain HTML form derived from the model's fields).
- An **edit** page per row, and a **delete** confirmation page.

All of it runs against the exact same table `tango migrate` created in part 2 — there's no separate admin-specific schema.

## What you've built

Starting from an empty directory, you now have one running application serving:

- JSON routes (`/posts/`, `/posts/{id}/`) backed by real persistence.
- An HTML admin (`/admin/post/`) for the same data, behind Basic Auth.
- A schema created entirely through migrations you generated and applied yourself.

From here:

- The [guides](../guides/) cover each topic (routing, models, persistence, migrations, admin, checks, dialects) independently, if you want depth on one without redoing this tutorial.
- The [API reference](../reference.md) documents the full supported v0.1 surface.
- [Limitations and compatibility](../limitations.md) is worth reading before using tanGO for anything beyond a small project.
