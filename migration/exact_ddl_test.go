package migration

import (
	"context"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

func exactTable() CreateTable {
	return CreateTable{Table: "users", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true, Width: 32},
		{Name: "email", Type: "varchar", Length: 255, NotNull: true, Unique: true, UniqueName: "idx_users_email"},
		{Name: "language", Type: "varchar", Length: 5, DefaultKind: "string", DefaultValue: "en"},
		{Name: "is_active", Type: "boolean", NotNull: true, DefaultKind: "boolean", DefaultValue: "false"},
		{Name: "sort_order", Type: "integer", Width: 32, DefaultKind: "integer", DefaultValue: "0"},
		{Name: "notes", Type: "text", DefaultKind: "string", DefaultValue: "it's"},
		{Name: "slug", Type: "varchar", Length: 100, Indexed: true, IndexName: "ix_users_slug"},
		{Name: "created_at", Type: "timestamp", Naive: true, NotNull: true},
		{Name: "seen_at", Type: "timestamp"},
	}}
}

func TestCreateTableSQLExactSchemaPostgres(t *testing.T) {
	testdb.SQLiteOnly(t, "pure DDL string test")
	got := createTableSQL(db.Postgres, exactTable())
	want := []string{
		`CREATE TABLE "users" (` + strings.Join([]string{
			`"id" SERIAL PRIMARY KEY`,
			`"email" VARCHAR(255) NOT NULL`,
			`"language" VARCHAR(5) DEFAULT 'en'`,
			`"is_active" BOOLEAN NOT NULL DEFAULT FALSE`,
			`"sort_order" INTEGER DEFAULT 0`,
			`"notes" TEXT DEFAULT 'it''s'`,
			`"slug" VARCHAR(100)`,
			`"created_at" TIMESTAMP NOT NULL`,
			`"seen_at" TIMESTAMPTZ`,
		}, ", ") + `)`,
		`CREATE UNIQUE INDEX "idx_users_email" ON "users" ("email")`,
		`CREATE INDEX "ix_users_slug" ON "users" ("slug")`,
	}
	assertStatements(t, got, want)
}

func TestCreateTableSQLExactSchemaMySQL(t *testing.T) {
	testdb.SQLiteOnly(t, "pure DDL string test")
	got := createTableSQL(db.MySQL, exactTable())
	if !strings.Contains(got[0], "`id` INT AUTO_INCREMENT PRIMARY KEY") {
		t.Errorf("MySQL int4 key: %s", got[0])
	}
	for _, frag := range []string{
		"`sort_order` INT DEFAULT 0",
		"`notes` LONGTEXT DEFAULT ('it''s')",
		"`is_active` BOOLEAN NOT NULL DEFAULT FALSE",
		"`created_at` DATETIME(6) NOT NULL",
	} {
		if !strings.Contains(got[0], frag) {
			t.Errorf("MySQL CREATE TABLE lacks %q: %s", frag, got[0])
		}
	}
}

func TestCreateTableSQLLegacyColumnsUnchanged(t *testing.T) {
	testdb.SQLiteOnly(t, "pure DDL string test")
	legacy := CreateTable{Table: "post", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "slug", Type: "text", Unique: true},
		{Name: "published_at", Type: "timestamp"},
	}}
	want := []string{
		`CREATE TABLE "post" ("id" BIGSERIAL PRIMARY KEY, "slug" TEXT, "published_at" TIMESTAMPTZ)`,
		`CREATE UNIQUE INDEX "uniq_post_slug" ON "post" ("slug")`,
	}
	assertStatements(t, createTableSQL(db.Postgres, legacy), want)
}

func assertStatements(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d statements, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d:\n got  %s\n want %s", i, got[i], want[i])
		}
	}
}

func TestValidateColumnExactSchema(t *testing.T) {
	testdb.SQLiteOnly(t, "pure validation test")
	bad := []struct {
		name string
		col  Column
		want string
	}{
		{"width on text", Column{Name: "c", Type: "text", Width: 32}, "width"},
		{"width 64", Column{Name: "c", Type: "integer", Width: 64}, "width"},
		{"naive on text", Column{Name: "c", Type: "text", Naive: true}, "naive"},
		{"notnull pk", Column{Name: "c", Type: "integer", PrimaryKey: true, NotNull: true}, "primary key"},
		{"notnull with legacy default", Column{Name: "c", Type: "boolean", NotNull: true, Default: "TRUE"}, "legacy Default"},
		{"both defaults", Column{Name: "c", Type: "boolean", Default: "TRUE", DefaultKind: "boolean", DefaultValue: "true"}, "legacy Default"},
		{"kind mismatch", Column{Name: "c", Type: "text", DefaultKind: "boolean", DefaultValue: "true"}, "boolean default"},
		{"bad bool", Column{Name: "c", Type: "boolean", DefaultKind: "boolean", DefaultValue: "yes"}, "true or false"},
		{"bad int", Column{Name: "c", Type: "integer", DefaultKind: "integer", DefaultValue: "x"}, "integer"},
		{"int4 overflow", Column{Name: "c", Type: "integer", Width: 32, DefaultKind: "integer", DefaultValue: "3000000000"}, "range"},
		{"backslash", Column{Name: "c", Type: "text", DefaultKind: "string", DefaultValue: `a\b`}, "backslash"},
		{"too long", Column{Name: "c", Type: "varchar", Length: 2, DefaultKind: "string", DefaultValue: "abc"}, "longer"},
		{"unknown kind", Column{Name: "c", Type: "text", DefaultKind: "float", DefaultValue: "1"}, "kind"},
		{"value without kind", Column{Name: "c", Type: "text", DefaultValue: "x"}, "needs a DefaultKind"},
		{"unique name without unique", Column{Name: "c", Type: "text", UniqueName: "u"}, "UniqueName"},
		{"index name without index", Column{Name: "c", Type: "text", IndexName: "i"}, "IndexName"},
		{"bad index name", Column{Name: "c", Type: "text", Indexed: true, IndexName: "1x"}, "index name"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			err := validateColumn("t", tc.col)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateColumn = %v, want error containing %q", err, tc.want)
			}
		})
	}
	for _, c := range exactTable().Columns {
		if err := validateColumn("users", c); err != nil {
			t.Errorf("validateColumn(%s) = %v, want nil", c.Name, err)
		}
	}
}

// TestApplyStepExactSchemaCreateTable applies the exact-schema table on the
// test database and checks NOT NULL and DEFAULT really reach it.
func TestApplyStepExactSchemaCreateTable(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	if err := ApplyStep(ctx, sqlDB, dialect, exactTable()); err != nil {
		t.Fatalf("ApplyStep: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO users (email, created_at) VALUES ('a@x', '2026-01-02 03:04:05')"); err != nil {
		t.Fatalf("insert relying on defaults: %v", err)
	}
	var language, notes string
	var active bool
	var sortOrder int
	if err := sqlDB.QueryRowContext(ctx, "SELECT language, is_active, sort_order, notes FROM users").Scan(&language, &active, &sortOrder, &notes); err != nil {
		t.Fatalf("select: %v", err)
	}
	if language != "en" || active || sortOrder != 0 || notes != "it's" {
		t.Fatalf("defaults = (%q, %v, %d, %q), want (en, false, 0, it's)", language, active, sortOrder, notes)
	}
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO users (email) VALUES ('b@x')"); err == nil {
		t.Fatal("insert without created_at succeeded, want NOT NULL violation")
	}
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO users (email, created_at) VALUES ('a@x', '2026-01-02 03:04:05')"); err == nil {
		t.Fatal("duplicate email succeeded, want unique violation on idx_users_email")
	}
}

// TestApplyStepExactSchemaPostgresCatalog checks the catalog shape the
// angvpdotcom baseline depends on: an int4 key backed by an integer
// sequence, a timestamp without time zone, and the explicit index names.
func TestApplyStepExactSchemaPostgresCatalog(t *testing.T) {
	testdb.PostgresOnly(t, "reads the PostgreSQL catalog")
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	if err := ApplyStep(ctx, sqlDB, dialect, exactTable()); err != nil {
		t.Fatalf("ApplyStep: %v", err)
	}
	types := map[string]string{}
	rows, err := sqlDB.QueryContext(ctx, "SELECT column_name, data_type FROM information_schema.columns WHERE table_name = 'users'")
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			t.Fatalf("scan: %v", err)
		}
		types[name] = typ
	}
	for col, want := range map[string]string{
		"id":         "integer",
		"sort_order": "integer",
		"created_at": "timestamp without time zone",
		"seen_at":    "timestamp with time zone",
	} {
		if types[col] != want {
			t.Errorf("%s type = %q, want %q", col, types[col], want)
		}
	}
	var seqType string
	if err := sqlDB.QueryRowContext(ctx, "SELECT data_type FROM information_schema.sequences WHERE sequence_name = 'users_id_seq'").Scan(&seqType); err != nil {
		t.Fatalf("sequence: %v", err)
	}
	if seqType != "integer" {
		t.Errorf("users_id_seq AS %s, want integer", seqType)
	}
	for _, idx := range []string{"idx_users_email", "ix_users_slug"} {
		var n int
		if err := sqlDB.QueryRowContext(ctx, "SELECT count(*) FROM pg_indexes WHERE tablename = 'users' AND indexname = $1", idx).Scan(&n); err != nil || n != 1 {
			t.Errorf("index %s: count %d, err %v", idx, n, err)
		}
	}
}

// TestApplyStepExactSchemaNonASCIIDefaults checks a non-ASCII default reaches
// the database unchanged on a text and on a varchar column, whatever the
// connection character set.
func TestApplyStepExactSchemaNonASCIIDefaults(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	step := CreateTable{Table: "greetings", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true, Width: 32},
		{Name: "short", Type: "varchar", Length: 20, DefaultKind: "string", DefaultValue: "ñandú ✓"},
		{Name: "long", Type: "text", DefaultKind: "string", DefaultValue: "ñandú ✓"},
	}}
	if err := ApplyStep(ctx, sqlDB, dialect, step); err != nil {
		t.Fatalf("ApplyStep: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, "INSERT INTO greetings (id) VALUES (1)"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var short, long string
	if err := sqlDB.QueryRowContext(ctx, selectGreetings(dialect)).Scan(&short, &long); err != nil {
		t.Fatalf("select: %v", err)
	}
	if short != "ñandú ✓" || long != "ñandú ✓" {
		t.Fatalf("defaults = (%q, %q), want both %q", short, long, "ñandú ✓")
	}
}

func selectGreetings(dialect db.Dialect) string {
	if dialect == db.MySQL {
		return "SELECT `short`, `long` FROM greetings"
	}
	return `SELECT "short", "long" FROM greetings`
}

// TestApplyStepExactSchemaForeignKeyWidth checks an int4 foreign key to an
// int4 key is accepted on every dialect, the case ValidateModels allows.
func TestApplyStepExactSchemaForeignKeyWidth(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	ctx := context.Background()
	parent := CreateTable{Table: "parents", Columns: []Column{{Name: "id", Type: "integer", PrimaryKey: true, Width: 32}}}
	child := CreateTable{Table: "children", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true, Width: 32},
		{Name: "parent_id", Type: "integer", Width: 32, NotNull: true, References: "parents"},
	}}
	for _, step := range []Step{parent, child} {
		if err := ApplyStep(ctx, sqlDB, dialect, step); err != nil {
			t.Fatalf("ApplyStep(%T): %v", step, err)
		}
	}
}

func TestValidateAlterColumnTypeExactFields(t *testing.T) {
	testdb.SQLiteOnly(t, "pure validation test")
	bad := []struct {
		name string
		step AlterColumnType
		want string
	}{
		{"from width 64", AlterColumnType{Table: "t", Column: "c", From: "integer", To: "integer", FromWidth: 64}, "from width"},
		{"to width on text", AlterColumnType{Table: "t", Column: "c", From: "integer", To: "text", ToWidth: 32}, "to width"},
		{"naive on integer", AlterColumnType{Table: "t", Column: "c", From: "integer", To: "integer", FromNaive: true}, "naive"},
		{"bad default kind", AlterColumnType{Table: "t", Column: "c", From: "integer", To: "text", DefaultKind: "float", DefaultValue: "1"}, "unknown default kind"},
		{"value without kind", AlterColumnType{Table: "t", Column: "c", From: "integer", To: "text", DefaultValue: "1"}, "needs a DefaultKind"},
		{"bad boolean", AlterColumnType{Table: "t", Column: "c", From: "boolean", To: "text", DefaultKind: "boolean", DefaultValue: "yes"}, "true or false"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAlterLengths(tc.step)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validateAlterLengths = %v, want error containing %q", err, tc.want)
			}
		})
	}
	ok := AlterColumnType{Table: "t", Column: "c", From: "integer", To: "integer", FromWidth: 32, DefaultKind: "integer", DefaultValue: "0"}
	if err := validateAlterLengths(ok); err != nil {
		t.Errorf("validateAlterLengths(valid) = %v", err)
	}
}

func TestReplayNewStepsUpdateColumnState(t *testing.T) {
	testdb.SQLiteOnly(t, "pure replay test")
	create := CreateTable{Table: "users", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true, Width: 32},
		{Name: "email", Type: "varchar", Length: 255, Unique: true, UniqueName: "idx_users_email"},
		{Name: "created_at", Type: "timestamp", Naive: true},
	}}
	state, err := Replay([]Migration{{App: "a", Name: "0001", Up: []Step{
		create,
		AlterColumnNotNull{Table: "users", Column: "email", NotNull: true},
		AlterColumnDefault{Table: "users", Column: "created_at", DefaultKind: "string", DefaultValue: "x"},
		RenameIndex{Table: "users", Column: "email", Unique: true, From: "idx_users_email", To: "idx_users_mail"},
		AlterColumnType{Table: "users", Column: "id", From: "integer", To: "integer", FromWidth: 32},
		AlterColumnType{Table: "users", Column: "created_at", From: "timestamp", To: "timestamp", FromNaive: true},
	}}})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	cols := map[string]ColumnState{}
	for _, c := range state.Tables["users"].Columns {
		cols[c.Name] = c
	}
	if !cols["email"].NotNull || cols["email"].UniqueName != "idx_users_mail" {
		t.Errorf("email = %+v, want NotNull and UniqueName idx_users_mail", cols["email"])
	}
	if c := cols["created_at"]; c.DefaultKind != "string" || c.Naive {
		t.Errorf("created_at = %+v, want the default set and Naive cleared", c)
	}
	if cols["id"].Width != 0 {
		t.Errorf("id.Width = %d, want 0 after widening", cols["id"].Width)
	}

	// Renaming an index to the derived name records no explicit name.
	state, err = Replay([]Migration{{App: "a", Name: "0001", Up: []Step{
		create,
		RenameIndex{Table: "users", Column: "email", Unique: true, From: "idx_users_email", To: "uniq_users_email"},
	}}})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if name := state.Tables["users"].Columns[1].UniqueName; name != "" {
		t.Errorf("UniqueName = %q, want empty for the derived name", name)
	}
}
