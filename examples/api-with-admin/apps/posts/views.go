package posts

import (
	"strconv"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

func listPosts(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		query := db.Query{OrderBy: []string{"-CreatedAt"}}
		if authorID := ctx.Query("author_id"); authorID != "" {
			parsedID, err := strconv.ParseInt(authorID, 10, 64)
			if err != nil {
				return err
			}
			query.Where = []db.Condition{{Field: "AuthorID", Op: db.OpEq, Value: parsedID}}
		}
		// ?q= searches Title/Body as one OR group (db.Query.Any), ANDed with
		// the author_id filter above when both are present — the same
		// Where+Any composition admin's own search uses (see admin/list.go).
		if q := ctx.Query("q"); q != "" {
			pattern := "%" + q + "%"
			query.Any = []db.Condition{
				{Field: "Title", Op: db.OpLike, Value: pattern},
				{Field: "Body", Op: db.OpLike, Value: pattern},
			}
		}
		var posts []Post
		if err := store.List(ctx.Context(), meta, query, &posts); err != nil {
			return err
		}
		return ctx.JSON(200, posts)
	}
}

func getPost(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		var post Post
		if err := store.Get(ctx.Context(), meta, ctx.Param("id"), &post); err != nil {
			return err
		}
		return ctx.JSON(200, post)
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
