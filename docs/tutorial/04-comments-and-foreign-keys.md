# Tutorial, part 4: comments and foreign keys

Continuing from [part 3](03-admin.md), this part lets people comment on posts. You'll add a second model that points at `Post` through a foreign key, see what tanGO does when a comment references a post that doesn't exist, delete a post together with its comments, and drop down to raw SQL for a query `db.Store` doesn't cover. Along the way the views learn to answer with proper `404` and `400` responses instead of a generic `500`.

## A model with a foreign key

A foreign key field is named after the column it stores — `PostID`, with the same `ID` suffix as a primary key — and tagged with the model it points at:

```go
// apps/posts/models.go (as of part 4)
package posts

import "time"

type Post struct {
	ID        int64 `tango:"pk"`
	Title     string
	Body      string
	CreatedAt time.Time
}

type Comment struct {
	ID        int64 `tango:"pk"`
	PostID    int64 `tango:"fk=Post,index"`
	Author    string
	Body      string
	CreatedAt time.Time
}
```

Tags combine with commas: `fk=Post,index` declares the relationship and indexes the column, since every "comments for this post" lookup filters on it. tanGO has exactly one relationship shape — many-to-one — so there's no `post.Comments` accessor to declare on the other side. You ask for a post's comments explicitly, which you'll do below.

## Register it

Register `Comment` next to `Post`. While you're in the admin registration, give `Post` a `Label`: it's what the admin shows wherever a post is *referenced*, so the comment form's `PostID` field becomes a dropdown of post titles rather than bare numbers:

```go
// apps/posts/app.go (as of part 4)
func New(store *db.Store) tango.App {
	return tango.NewApp("posts", func(registry *tango.Registry) error {
		if err := registry.Models().Register(Post{}); err != nil {
			return err
		}
		if err := registry.Models().Register(Comment{}); err != nil {
			return err
		}
		if err := registry.Admin().Register(Post{}, admin.Options{
			ListDisplay: []string{"Title", "CreatedAt"},
			Search:      []string{"Title"},
			Ordering:    []string{"CreatedAt"},
			Label:       "Title",
		}); err != nil {
			return err
		}
		if err := registry.Admin().Register(Comment{}, admin.Options{
			ListDisplay: []string{"PostID", "Author", "CreatedAt"},
			Search:      []string{"Author", "Body"},
		}); err != nil {
			return err
		}

		postMeta, _ := registry.Models().Get("Post")
		commentMeta, _ := registry.Models().Get("Comment")
		return registry.Routes().Include("/posts/", tango.URLs{
			tango.Path("GET", "/", listPosts(store), tango.Name("list")),
			tango.Path("POST", "/", createPost(store, postMeta), tango.Name("create")),
			tango.Path("GET", "/{id}/", getPost(store, postMeta), tango.Name("detail")),
			tango.Path("DELETE", "/{id}/", deletePost(store, postMeta), tango.Name("delete")),
			tango.Path("GET", "/{id}/comments/", listComments(store, commentMeta), tango.Name("comments")),
			tango.Path("POST", "/{id}/comments/", createComment(store, commentMeta), tango.Name("comment")),
		})
	})
}
```

The order of the two `Register` calls doesn't matter. tanGO checks that `fk=Post` names a registered model once every installed app has finished registering, so a typo like `fk=Pots` fails `tango check` rather than surfacing at runtime.

Generate and apply the migration:

```sh
tango makemigrations
tango migrate
```

Open the new migration file and you'll find `References: "post"` on the `post_id` column: tanGO creates a real database-level foreign key constraint too. On SQLite that constraint is only enforced when foreign keys are switched on for the connection; `tango.LoadDBConfigFromEnv()` in your generated `main.go` switches them on whenever `TANGO_DB_DSN` names a SQLite database.

## Errors a client can act on

So far every view returns errors straight up, and tanGO turns any returned error into a generic `500`. That's the right default for failures the client can't fix — the real error is logged, never leaked. But "that post doesn't exist" and "you forgot the title" are the client's business. Add two small helpers at the bottom of `views.go`:

```go
// apps/posts/views.go
func notFound(ctx *tango.Context) error {
	return ctx.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
}

func badRequest(ctx *tango.Context, message string) error {
	return ctx.JSON(http.StatusBadRequest, map[string]string{"error": message})
}
```

`db.Store` reports a missing row as `db.ErrNotFound`, so `getPost` can tell the two cases apart with `errors.Is`:

```go
// apps/posts/views.go
func getPost(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		var post Post
		err := store.Get(ctx.Context(), meta, ctx.Param("id"), &post)
		if errors.Is(err, db.ErrNotFound) {
			return notFound(ctx)
		}
		if err != nil {
			return err
		}
		return ctx.JSON(http.StatusOK, post)
	}
}
```

`createPost` gets the same treatment, and fixes something parts 2 and 3 quietly left out: nothing ever set `CreatedAt`, so every post was created at the zero time. A view should also never let the client choose fields the server owns, like the ID:

```go
// apps/posts/views.go (as of part 4)
func createPost(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		var post Post
		if err := ctx.Bind(&post); err != nil {
			return badRequest(ctx, "invalid JSON body")
		}
		if post.Title == "" {
			return badRequest(ctx, "title is required")
		}
		post.ID = 0 // the database assigns IDs, never the client
		post.CreatedAt = time.Now().UTC()
		if err := store.Create(ctx.Context(), meta, &post); err != nil {
			return err
		}
		return ctx.JSON(http.StatusCreated, post)
	}
}
```

(Update the imports in `views.go` to `errors`, `net/http`, `strconv`, `time`, plus the three tanGO packages you already use.)

## Creating and listing comments

A comment's `PostID` comes from the URL, never from the request body — otherwise a client could post to `/posts/1/comments/` and attach the comment to post 2:

```go
// apps/posts/views.go
func createComment(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		postID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
		if err != nil {
			return notFound(ctx)
		}
		var comment Comment
		if err := ctx.Bind(&comment); err != nil {
			return badRequest(ctx, "invalid JSON body")
		}
		if comment.Body == "" {
			return badRequest(ctx, "body is required")
		}
		comment.ID = 0
		comment.PostID = postID // from the URL, never the body
		comment.CreatedAt = time.Now().UTC()

		err = store.Create(ctx.Context(), meta, &comment)
		if errors.Is(err, db.ErrInvalidForeignKey) {
			return notFound(ctx) // no post with that ID
		}
		if err != nil {
			return err
		}
		return ctx.JSON(http.StatusCreated, comment)
	}
}
```

You don't have to look the post up first. Before writing, `store.Create` checks that every foreign key field points at a row that exists, and returns `db.ErrInvalidForeignKey` if one doesn't — which is exactly "there's no post to comment on".

Listing a post's comments is a filtered `List`. Conditions name Go fields, not columns:

```go
// apps/posts/views.go
func listComments(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		postID, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
		if err != nil {
			return notFound(ctx)
		}
		var comments []Comment
		err = store.List(ctx.Context(), meta, db.Query{
			Where:   []db.Condition{{Field: "PostID", Op: db.OpEq, Value: postID}},
			OrderBy: []string{"CreatedAt"},
		}, &comments)
		if err != nil {
			return err
		}
		return ctx.JSON(http.StatusOK, comments)
	}
}
```

## Deleting a post deletes its comments

```go
// apps/posts/views.go (as of part 4)
func deletePost(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		var post Post
		err := store.Get(ctx.Context(), meta, ctx.Param("id"), &post)
		if errors.Is(err, db.ErrNotFound) {
			return notFound(ctx)
		}
		if err != nil {
			return err
		}
		// Deleting a post cascades: its comments are deleted first, in the same transaction.
		if err := store.Delete(ctx.Context(), meta, post.ID); err != nil {
			return err
		}
		ctx.ResponseWriter().WriteHeader(http.StatusNoContent)
		return nil
	}
}
```

`store.Delete` finds every row that references the post through a foreign key, deletes those first, then deletes the post — all in one transaction. This is the same behavior as Django's `on_delete=CASCADE`, and like Django it happens in the framework, not the database: the database constraint stays `RESTRICT`, so a raw `DELETE FROM post` that bypasses `store.Delete` is refused instead of leaving orphaned comments behind (on SQLite, by any connection that has foreign keys switched on). Deleting a post in the admin cascades the same way.

## When `db.Store` isn't enough: raw SQL

The post list would be more useful with a comment count per post. That's a join plus an aggregate, which is past what `db.Store`'s typed CRUD does — by design. `store.Query` runs any SQL you write and scans each row into a struct, matching columns to fields by name:

```go
// apps/posts/views.go (as of part 4)
// postSummary is one row of the list endpoint: a post plus how many
// comments it has. It isn't a registered model — just a shape to scan into.
type postSummary struct {
	ID           int64
	Title        string
	CreatedAt    time.Time
	CommentCount int
}

func listPosts(store *db.Store) tango.View {
	return func(ctx *tango.Context) error {
		var posts []postSummary
		err := store.Query(ctx.Context(), &posts, `
			SELECT p.id, p.title, p.created_at, COUNT(c.id) AS comment_count
			FROM post p
			LEFT JOIN comment c ON c.post_id = p.id
			GROUP BY p.id, p.title, p.created_at
			ORDER BY p.created_at DESC`)
		if err != nil {
			return err
		}
		return ctx.JSON(http.StatusOK, posts)
	}
}
```

Table and column names are the snake_case forms of your model and field names: `Post` is `post`, `CreatedAt` is `created_at`. `listPosts` no longer needs the `Post` metadata, which is why its route above passes only `store`. This query has no parameters; when yours do, use the placeholder syntax of your dialect (`?` on SQLite, `$1` on Postgres) — `store.Query` passes the SQL through untouched.

## Try it

```sh
go run .
curl -X POST http://localhost:8000/posts/ -d '{"title":"Welcome","body":"Say hi below."}'
# {"ID":2,"Title":"Welcome","Body":"Say hi below.","CreatedAt":"2026-09-28T02:42:26.861367Z"}

curl -X POST http://localhost:8000/posts/2/comments/ -d '{"author":"ana","body":"Hi!"}'
curl http://localhost:8000/posts/
# [{"ID":2,"Title":"Welcome","CreatedAt":"...","CommentCount":1}, ...]

curl -i -X POST http://localhost:8000/posts/99/comments/ -d '{"author":"x","body":"y"}'
# HTTP/1.1 404 Not Found   {"error":"not found"}

curl -i -X DELETE http://localhost:8000/posts/2/
# HTTP/1.1 204 No Content
curl http://localhost:8000/posts/2/comments/
# []   (the comment went with the post)
```

Your IDs may differ from the ones shown. In the admin, `/admin/comment/` now lists comments with the post's title in the `PostID` column, and the create form offers a dropdown of posts.

**Part 5** puts a face on the board: server-rendered HTML pages built with `html/template`.

Continue: [Tutorial, part 5: HTML pages](05-html-pages.md)
