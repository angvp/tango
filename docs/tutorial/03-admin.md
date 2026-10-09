# Tutorial, part 3: admin

Continuing from [part 2](02-persistence-migrations.md), this section registers `Post` with tanGO's HTML admin, giving you list, create, edit, and delete pages without writing any HTML yourself.

## Register the model with the admin

Admin registration is separate from model registration, and lives alongside it in `posts.New`'s `Register` function:

```go
// apps/posts/app.go (as of part 3)
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

`tango newproject` already installed the admin app in `project/project.go`, after your project apps. Keep `admin.New(store)` after `posts.New(store)` in `InstalledApps` — order matters, since the admin app reads what's already been registered with `registry.Admin()` when it runs.

Unlike earlier tanGO versions, there's no password baked into a generated file: admin accounts live in the database, created with the `tango admin` CLI. Apply the migration `tango makemigrations` generated for `AdminUser`/`AdminSession` (part of the same `tango migrate` you already ran in part 2), then create your first account:

```sh
tango migrate
tango admin create admin
```

It prompts for a password on stdin, without showing what you type — never a flag, so it doesn't end up in shell history.

## Run it

```sh
go run .
```

Visit `http://localhost:8000/admin/post/` — you'll be redirected to `/admin/login/` first, since admin auth is a real session-cookie login, not a browser-native Basic Auth prompt. Log in with the username and password from `tango admin create`, and you'll land back on the page you asked for. You get:

- A **list** page with your configured columns, a true row count and page-number pagination, and a search box.
- A **create** page (a plain HTML form derived from the model's fields, with humanized labels — `CreatedAt` shows as "Created At").
- An **edit** page per row, and a **delete** confirmation page.

All of it comes styled with tanGO's default admin theme out of the box — a dark sidebar listing every registered model, light content cards, no CSS to write yourself — and runs against the exact same table `tango migrate` created in part 2; there's no separate admin-specific schema. Visit `http://localhost:8000/admin/` (no model name needed) and it redirects you to whichever model sorts first alphabetically.

## Look at the data from the shell

The admin is one way to see your data. `tango shell` is another: a Go prompt with your project's apps and models loaded, and nothing served, so it works whether or not the server is running.

```sh
tango shell
```

Models are addressed as `app.Model`, and a row is a `map[string]any` keyed by field name. Here is a session on a fresh database (the IDs and counts in yours will differ if you created posts in part 2):

```text
tango> Models()
["admin.AdminSession" "admin.AdminUser" "posts.Post"]
tango> Create("posts.Post", map[string]any{"Title": "Hello", "Body": "From the shell"})
map[Body:"From the shell" ID:1 Title:"Hello"]
tango> Create("posts.Post", map[string]any{"Title": "Draft", "Body": "Not ready"})
map[Body:"Not ready" ID:2 Title:"Draft"]
tango> Count("posts.Post")
2
tango> List("posts.Post", map[string]any{"where": map[string]any{"Title": "Hello"}, "order": []string{"-ID"}, "limit": 10})
map[Body:"From the shell" ID:1 Title:"Hello"]
tango> Update("posts.Post", 2, map[string]any{"Title": "Draft, revised"})
map[Body:"Not ready" ID:2 Title:"Draft, revised"]
tango> rows, err := List("posts.Post")
tango> len(rows)
2
tango> Delete("posts.Post", 2)
tango> Count("posts.Post")
1
```

`Models()` lists what is registered, including the admin's own tables. `Create`, `Get`, `List`, `Count`, `Update` and `Delete` do what they say, and a query is a plain map with `where` (equality), `order` (`"-ID"` is descending), `limit` and `offset`. `rows, err := List(...)` followed by `len(rows)` shows that it is ordinary Go: variables stay around from one line to the next. `exit()`, `quit()` or Ctrl-D leaves.

The shell runs your code with your project's database credentials. It is a developer tool, not a sandbox and not a way to administer a server remotely. The [shell guide](../guides/shell.md) covers the rest: `--readonly`, `-c` for scripts, your own helpers, and what Go the interpreter cannot run.

## What you've built

Starting from an empty directory, you now have one running application serving:

- JSON routes (`/posts/`, `/posts/{id}/`) backed by real persistence.
- An HTML admin (`/admin/post/`) for the same data, behind a real session-cookie login.
- A shell (`tango shell`) for looking at and changing the same data from a prompt.
- A schema created entirely through migrations you generated and applied yourself.

**Part 4** lets people comment on posts, which brings in foreign keys, cascading deletes, and raw SQL for the queries `db.Store` doesn't cover. Or, if you'd rather branch off here:

- The [guides](../guides/) cover each topic (routing, models, persistence, migrations, admin, checks, dialects) independently, if you want depth on one without redoing this tutorial.
- Two guides go beyond what this tutorial builds: [relationships and admin foreign keys](../guides/relationships-and-admin-foreign-keys.md) (giving `Post` an `AuthorID`-style foreign key, with cascade delete and FK-aware admin editing) and [reusable apps](../guides/reusable-apps.md) (packaging an app as its own importable Go package other projects can install).
- Once this app's `views.go`/`models.go` start feeling crowded, [application architecture](../guides/application-architecture.md) covers the Medium and Hexagonal shapes to grow into — and, just as importantly, when to stay exactly as small as this tutorial leaves you.
- The [API reference](../reference.md) documents the full supported surface.
- [Limitations](../limitations.md) and [versioning and compatibility](../compatibility.md) are worth reading before using tanGO for anything beyond a small project.

Continue: [Tutorial, part 4: comments and foreign keys](04-comments-and-foreign-keys.md)
