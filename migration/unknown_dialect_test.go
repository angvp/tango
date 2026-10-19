package migration

import (
	"context"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
)

// unknownDialect is a value no released Dialect has. Every public entry point
// must refuse it, not quietly generate SQLite SQL.
const unknownDialect = db.Dialect(99)

func TestEntryPointsRefuseAnUnknownDialect(t *testing.T) {
	ctx := context.Background()
	migrations := []Migration{{App: "a", Name: "0001_x", Up: []Step{CreateTable{Table: "t", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true}}}}}}
	cases := map[string]func() error{
		"ApplyStep":           func() error { return ApplyStep(ctx, nil, unknownDialect, DropTable{Table: "t"}) },
		"EnsureTrackingTable": func() error { return EnsureTrackingTable(ctx, nil, unknownDialect) },
		"ApplyPending":        func() error { return ApplyPending(ctx, nil, unknownDialect, migrations) },
		"RollbackLast":        func() error { return RollbackLast(ctx, nil, unknownDialect, migrations) },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("want an error for an unknown dialect, got nil")
			}
			if !strings.Contains(err.Error(), "unsupported dialect") {
				t.Fatalf("error %q should name the unsupported dialect", err)
			}
		})
	}
}
