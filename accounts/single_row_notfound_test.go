package accounts

import (
	"context"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

// The single-row lookups read with Store.First; a missing row is "no row",
// not an error.
func TestSingleRowLookupsReportNoRowWithoutError(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	migrationtest.Apply(t, sqlDB, dialect, func() []model.ModelMeta {
		metas := modelMetas()
		out := make([]model.ModelMeta, 0, len(metas))
		for _, m := range metas {
			out = append(out, m)
		}
		return out
	}())
	store := db.NewStore(sqlDB, dialect)
	ctx := context.Background()

	if _, ok, err := findAccountByEmail(ctx, store, "nobody@example.com"); ok || err != nil {
		t.Errorf("findAccountByEmail = ok %v, err %v, want no row and no error", ok, err)
	}
	if _, ok, err := accountFromToken(ctx, store, "unknown-token"); ok || err != nil {
		t.Errorf("accountFromToken = ok %v, err %v", ok, err)
	}
	m := &mailer{store: store, cfg: accountsConfig{now: time.Now}}
	if _, _, ok, err := m.usableToken(ctx, "unknown-token", PurposePasswordReset); ok || err != nil {
		t.Errorf("usableToken = ok %v, err %v", ok, err)
	}
}
