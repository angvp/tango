package posts

import (
	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

func listPosts(store *db.Store, meta model.ModelMeta) tango.View {
	return func(ctx *tango.Context) error {
		var posts []Post
		if err := store.List(ctx.Context(), meta, db.Query{OrderBy: []string{"-CreatedAt"}}, &posts); err != nil {
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
