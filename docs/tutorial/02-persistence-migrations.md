# Tutorial, part 2: persistence and migrations

Continuing from [part 1](01-bootstrap-routing-json.md), this section adds a real model, persists it via `db.Store`, and generates/applies the schema through migrations — no hand-written DDL.

## Define a model

Models are plain Go structs. Add a `Post` type to `apps/posts`:

```go
// apps/posts/models.go
package posts

import "time"

type Post struct {
	ID        int64  `tango:"pk"`
	Title     string
	Body      string
	CreatedAt time.Time
}
```

`tango:"pk"` marks the primary key explicitly — tanGO never guesses based on a field named `ID`. `tango:"unique"` and `tango:"index"` are also available; see the [models and tags guide](../guides/models-and-tags.md) for the full set and supported field kinds.

## Wire a store into the app

`main.go` already opens `*sql.DB` and constructs a `db.Store` (from part 1's scaffolding). To let `posts`'s views use it, change the app from a bare struct to a constructor that closes over the store — the same pattern tanGO's own admin app uses:

```go
// apps/posts/app.go
package posts

import "github.com/angvp/tango"
import "github.com/angvp/tango/db"

func New(store *db.Store) tango.App {
	return tango.NewApp("posts", func(registry *tango.Registry) error {
		if err := registry.Models().Register(Post{}); err != nil {
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

Views can't reach the registry themselves — `Context` deliberately exposes only the request/response surface (`Param`, `Query`, `Bind`, `JSON`, `Redirect`, plus raw escape hatches), not the framework's internals. So `ModelMeta` (looked up once, after registration) and `store` are both captured by closure when building each route's view, exactly like tanGO's own admin package does internally.

Update `main.go` to construct the store before building the app list, and use `posts.New(store)`:

```go
sqlDB, err := sql.Open("sqlite", "app.db")
// ... error handling ...
store := db.NewStore(sqlDB, db.SQLite)

config := tango.Config{
	InstalledApps: []tango.App{posts.New(store)},
	Addr:          ":8000",
}
```

(`tango newproject`'s generated `main.go` constructs the store later, right before starting the server; move that construction earlier so it's available when building `InstalledApps`. This is the kind of one-line rewiring the "no hidden setup" principle expects you to do by hand.)

## CRUD views

`db.Store`'s `Create`/`Get`/`List`/`Update`/`Delete` take the model's `ModelMeta` (looked up from the registry) and a destination value:

```go
func listPosts(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		var posts []Post
		if err := store.List(ctx.Context(), meta, db.Query{OrderBy: []string{"-CreatedAt"}}, &posts); err != nil {
			return err
		}
		return ctx.JSON(200, posts)
	}
}

func createPost(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		var post Post
		if err := ctx.Bind(&post); err != nil {
			return err
		}
		if err := store.Create(ctx.Context(), meta, &post); err != nil {
			return err
		}
		return ctx.JSON(201, post)
	}
}
```

(`getPost` follows the same shape, using `store.Get(ctx.Context(), meta, ctx.Param("id"), &post)`. Remember to add `"github.com/angvp/tango/model"` to the imports for the `model.ModelMeta` parameter type.)

> `db.Store` also exposes `Query`/`QueryRow` raw-SQL escape hatches when the metadata-driven CRUD surface isn't enough — see the [persistence guide](../guides/persistence-crud-and-raw-sql.md).

## Generate and apply a migration

`tango makemigrations` diffs your registered models against the *replayed* state of existing migration files — there's no separate schema snapshot to go stale:

```sh
tango makemigrations
# created migrations/0001_auto.go
```

Inspect it — it's plain Go, not a DSL, expressing typed steps like `migration.CreateTable`. Apply it:

```sh
tango migrate
# migrations applied
```

This records one row per applied migration in a `tango_migrations` table (`app`, `name`, `applied_at`), Django-inspired like `django_migrations`. Run `tango check` again to confirm registration still passes, then exercise the new routes:

```sh
go run .
curl -X POST http://localhost:8000/posts/ -d '{"title":"Hello","body":"First post"}'
curl http://localhost:8000/posts/
```

## Rolling back

```sh
tango migrate down
# rolled back last migration
```

This runs the migration's generated `Down` steps and removes its `tango_migrations` row. Not every migration can be rolled back: one containing a lossy step (`DropColumn`, `DropTable`) is marked **irreversible**, and `tango migrate down` on it fails explicitly rather than silently discarding data it can't restore.

**Part 3** registers `Post` with the HTML admin, giving you list/create/edit/delete pages for free.

Continue: [Tutorial, part 3: admin](03-admin.md)
