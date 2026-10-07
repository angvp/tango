package posts

import (
	"context"
	"time"

	"github.com/angvp/tango/db"
)

// PostSummary is one post plus how many comments it has. It isn't a
// registered model — just a shape to scan query results into.
type PostSummary struct {
	ID           int64
	Title        string
	CreatedAt    time.Time
	CommentCount int
}

// Summaries returns every post, newest first, with its comment count.
func Summaries(ctx context.Context, store *db.Store) ([]PostSummary, error) {
	var posts []PostSummary
	err := store.Query(ctx, &posts, `
		SELECT p.id, p.title, p.created_at, COUNT(c.id) AS comment_count
		FROM post p
		LEFT JOIN comment c ON c.post_id = p.id
		GROUP BY p.id, p.title, p.created_at
		ORDER BY p.created_at DESC`)
	return posts, err
}
