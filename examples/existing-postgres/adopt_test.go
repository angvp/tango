package main

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"

	"existing-postgres/ledger"
	"existing-postgres/migrations"
)

//go:embed legacy.sql
var legacySQL string

// ledgerBaseline is the generated migration that reproduces legacy.sql.
func ledgerBaseline(t *testing.T) migration.Migration {
	t.Helper()
	for _, m := range migrations.Migrations {
		if m.App == "ledger" {
			return m
		}
	}
	t.Fatal("no ledger migration")
	return migration.Migration{}
}

// ledgerModels is the registry main builds, read back as metadata.
func ledgerModels(t *testing.T) []model.ModelMeta {
	t.Helper()
	registry, err := tango.BuildRegistry(appConfig(testdb.Store(t)))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	var metas []model.ModelMeta
	for _, name := range []string{"Customers", "Invoices"} {
		meta, ok := registry.Models().Get(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		metas = append(metas, meta)
	}
	return metas
}

// TestTheModelsDescribeTheBaselineExactly is the guarantee that makes
// adoption safe: replaying the generated migrations and comparing them with
// the models finds nothing to change, so `tango makemigrations` writes no
// file for a database that already exists. The store only lets the models register; nothing is
// read from it.
func TestTheModelsDescribeTheBaselineExactly(t *testing.T) {
	state, err := migration.Replay(migrations.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := migration.Diff(ledgerModels(t), state)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range changes {
		if m.App == "ledger" {
			t.Fatalf("the ledger models differ from their baseline: %+v", m.Up)
		}
	}
}

// schemaDump lists every column and index of the tables in one schema, in a
// stable order, so two schemas can be compared line by line.
func schemaDump(t *testing.T, sqlDB *sql.DB, schema string) []string {
	t.Helper()
	var out []string
	rows, err := sqlDB.Query(`SELECT table_name, column_name, data_type, is_nullable, COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name IN ('customers', 'invoices')
		ORDER BY table_name, ordinal_position`, schema)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table, column, dataType, nullable, def string
		if err := rows.Scan(&table, &column, &dataType, &nullable, &def); err != nil {
			t.Fatal(err)
		}
		def = strings.ReplaceAll(def, schema+".", "") // nextval('legacy.customers_id_seq')
		out = append(out, fmt.Sprintf("column %s.%s %s null=%s default=%s", table, column, dataType, nullable, def))
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	idx, err := sqlDB.Query(`SELECT indexname, indexdef FROM pg_indexes
		WHERE schemaname = $1 AND tablename IN ('customers', 'invoices') ORDER BY indexname`, schema)
	if err != nil {
		t.Fatal(err)
	}
	for idx.Next() {
		var name, def string
		if err := idx.Scan(&name, &def); err != nil {
			t.Fatal(err)
		}
		out = append(out, "index "+name+": "+strings.ReplaceAll(def, schema+".", ""))
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// TestTheBaselineBuildsTheLegacySchema runs the legacy SQL in one schema and
// the generated baseline in another, on PostgreSQL, and compares them: the
// same columns, types, nullability, defaults and index names.
func TestTheBaselineBuildsTheLegacySchema(t *testing.T) {
	testdb.PostgresOnly(t, "compares information_schema and pg_indexes")
	sqlDB, dialect := testdb.Open(t)
	sqlDB.SetMaxOpenConns(1) // one session, so search_path sticks

	// The schemas live beside the test database's own, so remove them whatever
	// the test does, including a failed earlier run.
	dropSchemas := func() {
		_, _ = sqlDB.Exec(`SET search_path TO public`)
		_, _ = sqlDB.Exec(`DROP SCHEMA IF EXISTS legacy, fresh CASCADE`)
	}
	dropSchemas()
	t.Cleanup(dropSchemas)
	for _, schema := range []string{"legacy", "fresh"} {
		if _, err := sqlDB.Exec(`CREATE SCHEMA ` + schema); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sqlDB.Exec(`SET search_path TO legacy`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(legacySQL); err != nil {
		t.Fatalf("legacy.sql: %v", err)
	}
	if _, err := sqlDB.Exec(`SET search_path TO fresh`); err != nil {
		t.Fatal(err)
	}
	if err := migration.ApplyPending(t.Context(), sqlDB, dialect, []migration.Migration{ledgerBaseline(t)}); err != nil {
		t.Fatalf("baseline: %v", err)
	}

	legacy, fresh := schemaDump(t, sqlDB, "legacy"), schemaDump(t, sqlDB, "fresh")
	if len(legacy) == 0 {
		t.Fatal("the legacy schema is empty")
	}
	if strings.Join(legacy, "\n") != strings.Join(fresh, "\n") {
		t.Fatalf("the baseline does not rebuild the legacy schema.\nlegacy:\n%s\n\nbaseline:\n%s",
			strings.Join(legacy, "\n"), strings.Join(fresh, "\n"))
	}
}

// TestAnExistingDatabaseAdoptsTheBaselineWithoutRunningIt is the whole
// adoption: the tables exist and hold data, the baseline is recorded as
// applied with one INSERT, and migrate then applies only what is new (the
// admin's tables). The rows read back through tanGO, including the Date and
// the nullable timestamp.
func TestAnExistingDatabaseAdoptsTheBaselineWithoutRunningIt(t *testing.T) {
	testdb.PostgresOnly(t, "adopts a PostgreSQL database that already has its tables")
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()

	if _, err := sqlDB.Exec(legacySQL); err != nil {
		t.Fatalf("legacy.sql: %v", err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO customers (name, email, since, created_at) VALUES ('Ada', 'ada@example.com', '2019-03-04', '2019-03-04 10:00:00')`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO invoices (customer_id, amount_cents, issued_on) VALUES (1, 1250, '2024-02-29')`); err != nil {
		t.Fatal(err)
	}

	// Without the record, migrate would try to create customers again.
	if err := migration.ApplyPending(ctx, sqlDB, dialect, migrations.Migrations); err == nil {
		t.Fatal("applying the baseline to a database that has the tables should fail")
	}

	// The adoption: record the baseline as applied, once, by hand.
	baseline := ledgerBaseline(t)
	if err := migration.EnsureTrackingTable(ctx, sqlDB, dialect); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO tango_migrations (app, name, applied_at) VALUES ($1, $2, now()) ON CONFLICT (app, name) DO NOTHING`, baseline.App, baseline.Name); err != nil {
		t.Fatal(err)
	}
	if err := migration.ApplyPending(ctx, sqlDB, dialect, migrations.Migrations); err != nil {
		t.Fatalf("migrate after adopting: %v", err)
	}

	store := db.NewStore(sqlDB, dialect)
	registry, err := tango.BuildRegistry(appConfig(store))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	customersMeta, _ := registry.Models().Get("Customers")
	var customers []ledger.Customers
	if err := store.List(ctx, customersMeta, db.Query{}, &customers); err != nil {
		t.Fatal(err)
	}
	if len(customers) != 1 || customers[0].Tier != "standard" {
		t.Fatalf("customers = %+v, want Ada on the default tier", customers)
	}
	if got := customers[0].Since.String(); got != "2019-03-04" {
		t.Errorf("Since = %q", got)
	}
	invoicesMeta, _ := registry.Models().Get("Invoices")
	var invoices []ledger.Invoices
	if err := store.List(ctx, invoicesMeta, db.Query{}, &invoices); err != nil {
		t.Fatal(err)
	}
	if len(invoices) != 1 || !invoices[0].PaidAt.IsZero() || invoices[0].IssuedOn.String() != "2024-02-29" {
		t.Fatalf("invoices = %+v, want one unpaid invoice issued 2024-02-29", invoices)
	}
}
