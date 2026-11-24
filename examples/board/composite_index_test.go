package main

import (
	"reflect"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"

	"board/migrations"
)

// TestCommentListingIndexCoversPostAndCreationTime shows the migrations leave
// the comment listing's composite index in the database with its columns in
// the declared order, on each dialect the run uses.
func TestCommentListingIndexCoversPostAndCreationTime(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	if err := migration.ApplyPending(t.Context(), sqlDB, dialect, migrations.Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	query := map[db.Dialect]string{
		db.SQLite: `SELECT ii.name FROM pragma_index_info('idx_comment_post_id_created_at') ii ORDER BY ii.seqno`,
		db.Postgres: `SELECT a.attname FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
			CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
			JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
			WHERE c.relname = 'idx_comment_post_id_created_at' ORDER BY k.ord`,
		db.MySQL: `SELECT column_name FROM information_schema.statistics WHERE table_schema = DATABASE()
			AND table_name = 'comment' AND index_name = 'idx_comment_post_id_created_at' ORDER BY seq_in_index`,
	}[dialect]
	rows, err := sqlDB.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal(err)
		}
		got = append(got, column)
	}
	if want := []string{"post_id", "created_at"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("idx_comment_post_id_created_at covers %v, want %v", got, want)
	}
}
