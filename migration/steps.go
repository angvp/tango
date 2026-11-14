package migration

// Step is a typed, dialect-agnostic schema migration operation. It is exported
// so generated migration files and the tango CLI can share one representation;
// do not hand-author Step values directly in application code.
type Step interface {
	isMigrationStep()
}

// Column describes the SQL-relevant shape of one table column. It is exported
// for generated migration files and CLI diffing, not as a hand-authored
// application API.
type Column struct {
	Name string
	Type string
	// Length is the maximum number of characters of a "varchar" column, and
	// 0 for every other type. It is an additive field: generated files
	// written before it existed leave it 0 and mean what they always meant.
	Length     int `json:",omitempty"`
	PrimaryKey bool
	Unique     bool
	Indexed    bool
	// References is the target table name for a foreign key column (from a
	// tango:"fk=X" tag), or "" for an ordinary column. The generated
	// constraint is always RESTRICT/NO ACTION: cascade delete is
	// implemented in application code by Store.Delete, never by the
	// database itself.
	References string
	// Default is a raw SQL literal (e.g. "TRUE") the column is given as a
	// NOT NULL DEFAULT when adding it to an existing table, so pre-existing
	// rows backfill to that value instead of NULL. Empty means no default:
	// the column is added exactly as before this field existed. There is no
	// struct-tag-driven way to set this — it is only ever hand-authored on a
	// specific generated migration where an existing model needs it.
	Default string

	// The exact-schema fields below are additive: their zero values keep a
	// column exactly as before they existed.

	// Width is 32 for a 4-byte integer (INTEGER, SERIAL for a key) and 0 for
	// the default 8-byte integer. Only an "integer" column may set it.
	Width int `json:",omitempty"`
	// Naive makes a "timestamp" column a timestamp without time zone.
	Naive bool `json:",omitempty"`
	// NotNull adds NOT NULL. A primary key is always NOT NULL already.
	NotNull bool `json:",omitempty"`
	// DefaultKind ("string", "boolean" or "integer") and DefaultValue are a
	// typed column default, rendered and quoted by tango for each dialect.
	// Unlike the legacy Default they apply on CreateTable too.
	DefaultKind  string `json:",omitempty"`
	DefaultValue string `json:",omitempty"`
	// UniqueName and IndexName replace the derived uniq_<table>_<col> and
	// idx_<table>_<col> index names. Empty means the derived name.
	UniqueName string `json:",omitempty"`
	IndexName  string `json:",omitempty"`
}

// CreateTable creates a table with the provided columns. It is exported for
// generated migration files; application code should use the tango CLI.
type CreateTable struct {
	Table   string
	Columns []Column
}

// DropTable removes a table. It is exported for generated migration files;
// application code should use the tango CLI.
type DropTable struct {
	Table string
}

// AddColumn adds a column to an existing table. It is exported for generated
// migration files; application code should use the tango CLI.
type AddColumn struct {
	Table  string
	Column Column
}

// DropColumn removes a column from an existing table. It is exported for
// generated migration files; application code should use the tango CLI.
type DropColumn struct {
	Table  string
	Column string
}

// AlterColumnUnique changes a column's uniqueness constraint. It is exported
// for generated migration files; application code should use the tango CLI.
type AlterColumnUnique struct {
	Table  string
	Column string
	Unique bool
	// Name is the explicit name of the unique index; empty means the derived
	// uniq_<table>_<column>. It is additive: files written without it mean
	// what they always meant.
	Name string `json:",omitempty"`
}

// CreateIndex creates a plain index for a table column. It is exported for
// generated migration files; application code should use the tango CLI.
type CreateIndex struct {
	Table  string
	Column string
	// Name is the explicit index name; empty means the derived
	// idx_<table>_<column>.
	Name string `json:",omitempty"`
}

// DropIndex removes a plain index for a table column. It is exported for
// generated migration files; application code should use the tango CLI.
type DropIndex struct {
	Table  string
	Column string
	// Name is the explicit index name; empty means the derived
	// idx_<table>_<column>.
	Name string `json:",omitempty"`
}

// RenameColumn renames a column, keeping its data, constraints and indexes
// (an index named after the column takes the new name). It comes from a
// Rename mapping given to tango makemigrations. It is exported for
// generated migration files; application code should use the tango CLI.
type RenameColumn struct {
	Table string
	From  string
	To    string
}

// RenameTable renames a model's table, keeping its rows, constraints and
// indexes (an index named after the table takes the new name); other
// tables' foreign keys follow it. It comes from a Rename mapping given to
// tango makemigrations. It is exported for generated migration files;
// application code should use the tango CLI.
type RenameTable struct {
	From string
	To   string
}

// AlterColumnType changes a column's type by a Widening type change,
// converting every row's value with explicit SQL for the dialect. Default,
// when set, is the column's existing default converted to the new type (a
// raw SQL literal); the column keeps NOT NULL. A migration containing it
// is irreversible. It is exported for generated migration files;
// application code should use the tango CLI.
type AlterColumnType struct {
	Table  string
	Column string
	From   string
	// FromLength and ToLength are the varchar lengths of From and To, and 0
	// for any other type. They are additive: files written without them
	// mean what they always meant.
	FromLength int `json:",omitempty"`
	To         string
	ToLength   int `json:",omitempty"`
	Default    string

	// FromWidth and ToWidth are 32 for a 4-byte integer and 0 otherwise;
	// FromNaive and ToNaive mark a timestamp without time zone. They let a
	// width or time-zone change be expressed as a type change. DefaultKind
	// and DefaultValue are the column's typed default in effect when the
	// step runs, which MySQL needs to restate when it rewrites the column.
	// All are additive.
	FromWidth    int    `json:",omitempty"`
	ToWidth      int    `json:",omitempty"`
	FromNaive    bool   `json:",omitempty"`
	ToNaive      bool   `json:",omitempty"`
	DefaultKind  string `json:",omitempty"`
	DefaultValue string `json:",omitempty"`
}

// AlterColumnNotNull sets or drops a column's NOT NULL. Setting it checks,
// before any DDL, that no row holds NULL. DefaultKind and DefaultValue are the
// column's typed default in effect when the step runs, carried so MySQL can
// restate it when it rewrites the column. It is exported for generated
// migration files; application code should use the tango CLI.
type AlterColumnNotNull struct {
	Table        string
	Column       string
	NotNull      bool
	DefaultKind  string `json:",omitempty"`
	DefaultValue string `json:",omitempty"`
}

// AlterColumnDefault sets a column's typed default, or drops it when
// DefaultKind is empty. It is exported for generated migration files;
// application code should use the tango CLI.
type AlterColumnDefault struct {
	Table        string
	Column       string
	DefaultKind  string `json:",omitempty"`
	DefaultValue string `json:",omitempty"`
}

// RenameIndex renames the unique (Unique) or plain index of a column from
// From to To, both effective names. Column and Unique let SQLite, which has no
// rename for an index, create it again. It is exported for generated
// migration files; application code should use the tango CLI.
type RenameIndex struct {
	Table  string
	Column string
	Unique bool
	From   string
	To     string
}

// Migration groups the up/down steps for one generated migration file. It is
// part of the runtime contract accepted by ApplyPending and RollbackLast, but
// application code should normally use values generated by tango makemigrations.
type Migration struct {
	App        string
	Name       string
	Up         []Step
	Down       []Step
	Reversible bool
}

func (CreateTable) isMigrationStep()        {}
func (DropTable) isMigrationStep()          {}
func (AddColumn) isMigrationStep()          {}
func (DropColumn) isMigrationStep()         {}
func (AlterColumnUnique) isMigrationStep()  {}
func (CreateIndex) isMigrationStep()        {}
func (DropIndex) isMigrationStep()          {}
func (RenameColumn) isMigrationStep()       {}
func (AlterColumnType) isMigrationStep()    {}
func (RenameTable) isMigrationStep()        {}
func (AlterColumnNotNull) isMigrationStep() {}
func (AlterColumnDefault) isMigrationStep() {}
func (RenameIndex) isMigrationStep()        {}
