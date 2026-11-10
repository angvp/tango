package posts

import (
	"context"
	"strings"
	"time"

	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// PostSummary is one post plus how many comments it has and the name of
// whoever wrote it. It isn't a registered model — just a shape to scan query
// results into. AccountID is the key the byline is loaded by; it stays out
// of the JSON, and Byline is the only owner field a reader sees.
type PostSummary struct {
	ID           int64
	Title        string
	CreatedAt    time.Time
	CommentCount int
	AccountID    int64 `json:"-"`
	Byline       string
}

// Summaries returns every post, newest first, with its comment count and
// byline. The byline is one more statement for the whole list: Related loads
// each distinct account once, however many posts it wrote.
func Summaries(ctx context.Context, store *db.Store, accountMeta model.ModelMeta) ([]PostSummary, error) {
	var posts []PostSummary
	err := store.Query(ctx, &posts, `
		SELECT p.id, p.title, p.created_at, p.account_id, COUNT(c.id) AS comment_count
		FROM post p
		LEFT JOIN comment c ON c.post_id = p.id
		GROUP BY p.id, p.title, p.created_at, p.account_id
		ORDER BY p.created_at DESC`)
	if err != nil {
		return nil, err
	}

	owners := make(map[int64]accounts.Account)
	if err := store.Related(ctx, posts, "AccountID", accountMeta, owners); err != nil {
		return nil, err
	}
	for i, post := range posts {
		if owner, ok := owners[post.AccountID]; ok {
			posts[i].Byline = bylineName(owner.Email)
		}
	}
	return posts, nil
}

// bylineName is the public name of an account: the part of its email before
// the @. A post with no owner has no byline.
func bylineName(email string) string {
	name, _, _ := strings.Cut(email, "@")
	return name
}
