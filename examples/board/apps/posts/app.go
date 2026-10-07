package posts

import (
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/auth/jwt"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/ratelimit"

	"board/apps/live"
)

func New(store *db.Store, tokens *jwt.Service, feed *live.Feed) tango.App {
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

		postLimiter, err := ratelimit.NewLimiter(ratelimit.Options{Limit: 10, Refill: 6 * time.Minute})
		if err != nil {
			return err
		}
		commentLimiter, err := ratelimit.NewLimiter(ratelimit.Options{Limit: 10, Refill: time.Minute})
		if err != nil {
			return err
		}

		postMeta, _ := registry.Models().Get("Post")
		commentMeta, _ := registry.Models().Get("Comment")
		accountMeta, _ := registry.Models().Get("Account")
		return registry.Routes().Include("/posts/", tango.URLs{
			tango.Path("GET", "/", listPosts(store), tango.Name("list")),
			tango.Path("POST", "/", jwt.Require(createPost(store, postMeta, accountMeta, feed)),
				tango.Name("create"),
				tango.Use(ratelimit.Middleware(postLimiter, accountKey))),
			tango.Path("GET", "/{id}/", getPost(store, postMeta), tango.Name("detail")),
			tango.Path("DELETE", "/{id}/", jwt.Require(deletePost(store, postMeta, accountMeta)), tango.Name("delete")),
			tango.Path("GET", "/{id}/comments/", listComments(store, commentMeta), tango.Name("comments")),
			tango.Path("POST", "/{id}/comments/", createComment(store, commentMeta),
				tango.Name("comment"),
				tango.Use(ratelimit.Middleware(commentLimiter, ratelimit.RemoteIPKey()))),
		}, tango.WithMiddleware(tokens.Middleware(jwt.BearerToken)))
	})
}
