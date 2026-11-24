package migration

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

// liveIndex is what a database reports about one multi-column index.
type liveIndex struct {
	Name    string
	Columns []string
	Unique  bool
}

// liveCompositeIndexes lists the multi-column indexes of table from the
// dialect's own catalog: ordered columns and uniqueness, sorted by name.
func liveCompositeIndexes(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, table string) []liveIndex {
	t.Helper()
	var query string
	switch dialect {
	case db.SQLite:
		query = `SELECT il.name, il."unique", ii.name FROM pragma_index_list($1) il
			JOIN pragma_index_info(il.name) ii WHERE il.origin = 'c' ORDER BY il.name, ii.seqno`
	case db.Postgres:
		query = `SELECT c.relname, i.indisunique, a.attname
			FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid JOIN pg_class t ON t.oid = i.indrelid
			JOIN pg_namespace n ON n.oid = t.relnamespace
			CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
			JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
			WHERE t.relname = $1 AND n.nspname = current_schema() AND NOT i.indisprimary
			ORDER BY c.relname, k.ord`
	case db.MySQL:
		query = `SELECT index_name, non_unique = 0, column_name FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = $1 AND index_name <> 'PRIMARY' AND index_name NOT LIKE 'fk\_%'
			ORDER BY index_name, seq_in_index`
	}
	rows, err := sqlDB.Query(query, table)
	if err != nil {
		t.Fatalf("list indexes of %q: %v", table, err)
	}
	defer rows.Close()
	byName := map[string]*liveIndex{}
	var order []string
	for rows.Next() {
		var name, column string
		var unique bool
		if err := rows.Scan(&name, &unique, &column); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if byName[name] == nil {
			byName[name] = &liveIndex{Name: name, Unique: unique}
			order = append(order, name)
		}
		byName[name].Columns = append(byName[name].Columns, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var out []liveIndex
	for _, name := range order {
		if len(byName[name].Columns) > 1 {
			out = append(out, *byName[name])
		}
	}
	return out
}

func compositeTable(t *testing.T, sqlDB *sql.DB, dialect db.Dialect, table string) {
	t.Helper()
	applySteps(t, sqlDB, dialect, CreateTable{Table: table, Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "a", Type: "integer"},
		{Name: "b", Type: "integer"},
		{Name: "label", Type: "text"},
	}})
}

func TestCompositeIndexCreateAndDropOnEveryDialect(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	compositeTable(t, sqlDB, dialect, "ci_basic")
	applySteps(t, sqlDB, dialect,
		CreateCompositeIndex{Table: "ci_basic", Columns: []string{"b", "a"}},
		CreateCompositeIndex{Table: "ci_basic", Columns: []string{"a", "b"}, Unique: true, Name: "ix_ci_basic_pair"},
	)
	want := []liveIndex{
		{Name: "idx_ci_basic_b_a", Columns: []string{"b", "a"}},
		{Name: "ix_ci_basic_pair", Columns: []string{"a", "b"}, Unique: true},
	}
	if got := liveCompositeIndexes(t, sqlDB, dialect, "ci_basic"); !reflect.DeepEqual(got, want) {
		t.Fatalf("indexes = %#v, want %#v", got, want)
	}

	applySteps(t, sqlDB, dialect,
		DropCompositeIndex{Table: "ci_basic", Columns: []string{"b", "a"}},
		DropCompositeIndex{Table: "ci_basic", Columns: []string{"a", "b"}, Unique: true, Name: "ix_ci_basic_pair"},
	)
	if got := liveCompositeIndexes(t, sqlDB, dialect, "ci_basic"); len(got) != 0 {
		t.Fatalf("indexes after the drops = %#v, want none", got)
	}
}

func TestCompositeUniqueIndexRefusesADuplicatePair(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	compositeTable(t, sqlDB, dialect, "ci_unique")
	applySteps(t, sqlDB, dialect, CreateCompositeIndex{Table: "ci_unique", Columns: []string{"a", "b"}, Unique: true})
	mustExec(t, sqlDB, "INSERT INTO ci_unique (a, b, label) VALUES (1, 1, 'x')")
	mustExec(t, sqlDB, "INSERT INTO ci_unique (a, b, label) VALUES (1, 2, 'x')")
	if _, err := sqlDB.Exec("INSERT INTO ci_unique (a, b, label) VALUES (1, 1, 'y')"); err == nil {
		t.Fatal("a second (1, 1) row was accepted by a composite unique index")
	}
	mustExec(t, sqlDB, "INSERT INTO ci_unique (a, b, label) VALUES (NULL, 1, 'y')")
	mustExec(t, sqlDB, "INSERT INTO ci_unique (a, b, label) VALUES (NULL, 1, 'z')")
}

func TestCompositeIndexOverTextMakesTheColumnIndexableOnMySQL(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	compositeTable(t, sqlDB, dialect, "ci_text")
	applySteps(t, sqlDB, dialect, CreateCompositeIndex{Table: "ci_text", Columns: []string{"a", "label"}, Unique: true})
	want := []liveIndex{{Name: "uniq_ci_text_a_label", Columns: []string{"a", "label"}, Unique: true}}
	if got := liveCompositeIndexes(t, sqlDB, dialect, "ci_text"); !reflect.DeepEqual(got, want) {
		t.Fatalf("indexes = %#v, want %#v", got, want)
	}
}

func TestCompositeIndexKeyTooLongNamesTheTableIndexAndAdvice(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	if dialect != db.MySQL {
		t.Skip("MySQL limits the length of an index key")
	}
	applySteps(t, sqlDB, dialect, CreateTable{Table: "ci_long", Columns: []Column{
		{Name: "id", Type: "integer", PrimaryKey: true},
		{Name: "p", Type: "varchar", Length: 255},
		{Name: "q", Type: "varchar", Length: 255},
		{Name: "r", Type: "varchar", Length: 255},
		{Name: "s", Type: "varchar", Length: 255},
	}})
	err := ApplyStep(context.Background(), sqlDB, dialect, CreateCompositeIndex{Table: "ci_long", Columns: []string{"p", "q", "r", "s"}})
	if err == nil {
		t.Skip("this MySQL accepts the key; nothing to wrap")
	}
	for _, want := range []string{`"ci_long"`, `"idx_ci_long_p_q_r_s"`, "p, q, r, s", "varchar"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q lacks %q", err, want)
		}
	}
}

func TestSQLiteRebuildKeepsCompositeIndexes(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	compositeTable(t, sqlDB, dialect, "ci_rebuild")
	applySteps(t, sqlDB, dialect,
		CreateCompositeIndex{Table: "ci_rebuild", Columns: []string{"a", "b"}, Unique: true},
		CreateCompositeIndex{Table: "ci_rebuild", Columns: []string{"b", "label"}, Name: "ix_keep"},
		AlterColumnNotNull{Table: "ci_rebuild", Column: "label", NotNull: false},
	)
	mustExec(t, sqlDB, "INSERT INTO ci_rebuild (a, b, label) VALUES (1, 1, 'x')")
	if _, err := sqlDB.Exec("INSERT INTO ci_rebuild (a, b, label) VALUES (1, 1, 'y')"); err == nil {
		t.Fatal("the composite unique index did not survive the operation")
	}
	if got := liveCompositeIndexes(t, sqlDB, dialect, "ci_rebuild"); len(got) != 2 {
		t.Fatalf("indexes = %#v, want both", got)
	}
}

func TestCreateCompositeIndexSQLGoldens(t *testing.T) {
	testdb.SQLiteOnly(t, "pure SQL text test")
	cols := []string{"post_id", "created_at"}
	for dialect, want := range map[db.Dialect][2]string{
		db.SQLite:   {"CREATE INDEX `idx_t_post_id_created_at` ON `t` (`post_id`, `created_at`)", "CREATE UNIQUE INDEX `uniq_t_post_id_created_at` ON `t` (`post_id`, `created_at`)"},
		db.Postgres: {`CREATE INDEX "idx_t_post_id_created_at" ON "t" ("post_id", "created_at")`, `CREATE UNIQUE INDEX "uniq_t_post_id_created_at" ON "t" ("post_id", "created_at")`},
		db.MySQL:    {"CREATE INDEX `idx_t_post_id_created_at` ON `t` (`post_id`, `created_at`)", "CREATE UNIQUE INDEX `uniq_t_post_id_created_at` ON `t` (`post_id`, `created_at`)"},
	} {
		if got := createCompositeIndexSQL(dialect, false, "idx_t_post_id_created_at", "t", cols); got != want[0] {
			t.Errorf("%v plain: %s, want %s", dialect, got, want[0])
		}
		if got := createCompositeIndexSQL(dialect, true, "uniq_t_post_id_created_at", "t", cols); got != want[1] {
			t.Errorf("%v unique: %s, want %s", dialect, got, want[1])
		}
	}
}
