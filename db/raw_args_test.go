package db_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// TestRawQueryRejectsZeroCalendarArguments: Store.Query and Store.QueryRow
// bind their arguments with the ORM's rule. A zero time or zero Date is not
// a value, so it is rejected with ErrInvalidQuery before any SQL, on every
// dialect; a nil argument, which is NULL, is passed through.
func TestRawQueryRejectsZeroCalendarArguments(t *testing.T) {
	zeros := []any{time.Time{}, time.Time{}.In(time.FixedZone("Z0", 0)), model.Date{}}
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres, db.MySQL} {
		for _, zero := range zeros {
			rec := &recorder{}
			store := db.NewStore(sql.OpenDB(rec), dialect)
			var rows []goldenPost
			err := store.Query(context.Background(), &rows, "SELECT id FROM golden_post WHERE created_at < ?", zero)
			if !errors.Is(err, db.ErrInvalidQuery) || len(rec.statements()) != 0 {
				t.Errorf("dialect %d, Query with %T: err = %v, statements = %d; want ErrInvalidQuery and no SQL", dialect, zero, err, len(rec.statements()))
			}
			var one goldenPost
			err = store.QueryRow(context.Background(), &one, "SELECT id FROM golden_post WHERE created_at < ?", zero)
			if !errors.Is(err, db.ErrInvalidQuery) || len(rec.statements()) != 0 {
				t.Errorf("dialect %d, QueryRow with %T: err = %v, statements = %d; want ErrInvalidQuery and no SQL", dialect, zero, err, len(rec.statements()))
			}
		}
	}
}
