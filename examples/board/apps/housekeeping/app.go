// Package housekeeping runs the board's background maintenance jobs.
package housekeeping

import (
	"context"
	"log/slog"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

func New(store *db.Store) tango.App {
	return tango.NewApp("housekeeping", func(registry *tango.Registry) error {
		sessionMeta, _ := registry.Models().Get("AccountSession")
		return registry.RegisterJob(tango.Job{
			Name:     "prune-expired-sessions",
			Interval: time.Hour,
			Run: func(ctx context.Context) error {
				return pruneSessions(ctx, store, sessionMeta)
			},
		})
	})
}

// pruneSessions deletes login sessions that have expired. They're already
// useless — an expired session never logs anyone in — but nothing else
// removes them, so without this the table only grows.
func pruneSessions(ctx context.Context, store *db.Store, sessionMeta model.ModelMeta) error {
	var expired []accounts.AccountSession
	err := store.List(ctx, sessionMeta, db.Query{
		Where: []db.Condition{{Field: "ExpiresAt", Op: db.OpLt, Value: time.Now().UTC()}},
		Limit: 500, // a bounded batch per run; the next run picks up the rest
	}, &expired)
	if err != nil {
		return err
	}
	for _, session := range expired {
		if err := store.Delete(ctx, sessionMeta, session.ID); err != nil {
			return err
		}
	}
	if len(expired) > 0 {
		slog.InfoContext(ctx, "pruned expired sessions", "count", len(expired))
	}
	return nil
}
