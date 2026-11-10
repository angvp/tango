package querycount_test

import (
	"context"
	"testing"

	"github.com/angvp/tango/internal/querycount"
	"github.com/angvp/tango/testdb"
)

// Statements sent inside a transaction are counted, and a Reset before the
// measured code keeps setup statements out of the count. Each zero here sits
// beside the positive count from the same run, so the instrument is shown to
// see the statements it is asked to count.
func TestCountsStatementsInsideATransaction(t *testing.T) {
	ctx := context.Background()
	counter := querycount.Attach(t)
	sqlDB, _ := testdb.Open(t)

	if _, err := sqlDB.ExecContext(ctx, "SELECT 0"); err != nil {
		t.Fatalf("setup statement: %v", err)
	}
	counter.Reset()

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "SELECT 1"); err != nil {
		t.Fatalf("exec in tx: %v", err)
	}
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT 2").Scan(&n); err != nil {
		t.Fatalf("query in tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	statements := counter.Statements()
	if len(statements) != 2 {
		t.Fatalf("statements = %q, want the two inside the transaction", statements)
	}
	if got := counter.Matching("SELECT 0"); got != 0 {
		t.Fatalf("setup statement counted %d times after Reset, want 0", got)
	}
	if got := counter.Matching("SELECT 1"); got != 1 {
		t.Fatalf("transaction exec counted %d times, want 1", got)
	}
}
