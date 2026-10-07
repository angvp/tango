package posts

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"

	"board/apps/live"
)

func listPosts(store *db.Store) tango.View {
	return func(ctx *tango.Context) error {
		posts, err := Summaries(ctx.Context(), store)
		if err != nil {
			return err
		}
		return ctx.JSON(http.StatusOK, posts)
	}
}

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

func createPost(store *db.Store, meta, accountMeta model.ModelMeta, feed *live.Feed) tango.View {
	return func(ctx *tango.Context) error {
		account, ok, err := tokenAccount(ctx, store, accountMeta)
		if err != nil {
			return err
		}
		if !ok {
			return unauthorized(ctx)
		}

		var post Post
		if err := ctx.Bind(&post); err != nil {
			return badRequest(ctx, "invalid JSON body")
		}
		if post.Title == "" {
			return badRequest(ctx, "title is required")
		}
		post.ID = 0                 // the database assigns IDs, never the client
		post.AccountID = account.ID // the poster is whoever the token belongs to
		post.CreatedAt = time.Now().UTC()
		if err := store.Create(ctx.Context(), meta, &post); err != nil {
			return err
		}
		if err := feed.Publish(ctx.Context(), post.ID, post.Title); err != nil {
			ctx.Logger().Warn("live feed publish failed", "post", post.ID, "err", err)
		}
		return ctx.JSON(http.StatusCreated, post)
	}
}

func deletePost(store *db.Store, meta, accountMeta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		account, ok, err := tokenAccount(ctx, store, accountMeta)
		if err != nil {
			return err
		}
		if !ok {
			return unauthorized(ctx)
		}

		var post Post
		err = store.Get(ctx.Context(), meta, ctx.Param("id"), &post)
		if errors.Is(err, db.ErrNotFound) {
			return notFound(ctx)
		}
		if err != nil {
			return err
		}
		if post.AccountID != account.ID {
			return ctx.JSON(http.StatusForbidden, map[string]string{"error": "you can only delete your own posts"})
		}
		// Deleting a post cascades: its comments are deleted first, in the same transaction.
		if err := store.Delete(ctx.Context(), meta, post.ID); err != nil {
			return err
		}
		ctx.ResponseWriter().WriteHeader(http.StatusNoContent)
		return nil
	}
}

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

func notFound(ctx *tango.Context) error {
	return ctx.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
}

func badRequest(ctx *tango.Context, message string) error {
	return ctx.JSON(http.StatusBadRequest, map[string]string{"error": message})
}
