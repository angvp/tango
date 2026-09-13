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

## Use the generated admin app

`tango newproject` already installed the admin app in `main.go`, after your project apps. Keep `admin.New(store)` after `posts.New(store)` in `InstalledApps` — order matters, since the admin app reads what's already been registered with `registry.Admin()` when it runs.

Unlike earlier tanGO versions, there's no password baked into a generated file: admin accounts live in the database, created with the `tango admin` CLI. Apply the migration `tango makemigrations` generated for `AdminUser`/`AdminSession` (part of the same `tango migrate` you already ran in part 2), then create your first account:

```sh
tango migrate
tango admin create admin
```

It prompts for a password on stdin — never a flag, so it doesn't end up in shell history.

## Run it

```sh
go run .
```

Visit `http://localhost:8000/admin/post/` — you'll be redirected to `/admin/login/` first, since admin auth is a real session-cookie login, not a browser-native Basic Auth prompt. Log in with the username and password from `tango admin create`, and you'll land back on the page you asked for. You get:

- A **list** page with your configured columns, a true row count and page-number pagination, and a search box.
- A **create** page (a plain HTML form derived from the model's fields, with humanized labels — `CreatedAt` shows as "Created At").
- An **edit** page per row, and a **delete** confirmation page.

All of it comes styled with tanGO's default admin theme out of the box — a dark sidebar listing every registered model, light content cards, no CSS to write yourself — and runs against the exact same table `tango migrate` created in part 2; there's no separate admin-specific schema. Visit `http://localhost:8000/admin/` (no model name needed) and it redirects you to whichever model sorts first alphabetically.

## What you've built

Starting from an empty directory, you now have one running application serving:

- JSON routes (`/posts/`, `/posts/{id}/`) backed by real persistence.
- An HTML admin (`/admin/post/`) for the same data, behind a real session-cookie login.
- A schema created entirely through migrations you generated and applied yourself.

From here:

- The [guides](../guides/) cover each topic (routing, models, persistence, migrations, admin, checks, dialects) independently, if you want depth on one without redoing this tutorial.
- Two guides go beyond what this tutorial builds: [relationships and admin foreign keys](../guides/relationships-and-admin-foreign-keys.md) (giving `Post` an `AuthorID`-style foreign key, with cascade delete and FK-aware admin editing) and [reusable apps](../guides/reusable-apps.md) (packaging an app as its own importable Go package other projects can install).
- The [API reference](../reference.md) documents the full supported v0.1 surface.
- [Limitations and compatibility](../limitations.md) is worth reading before using tanGO for anything beyond a small project.
