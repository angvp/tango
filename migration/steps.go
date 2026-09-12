package migration

// Step is a typed, dialect-agnostic schema migration operation.
type Step interface {
	isMigrationStep()
}

// Column describes the SQL-relevant shape of one table column.
type Column struct {
	Name       string
	Type       string
	PrimaryKey bool
	Unique     bool
	Indexed    bool
}

// CreateTable creates a table with the provided columns.
type CreateTable struct {
	Table   string
	Columns []Column
}

// DropTable removes a table.
type DropTable struct {
	Table string
}

// AddColumn adds a column to an existing table.
type AddColumn struct {
	Table  string
	Column Column
}

// DropColumn removes a column from an existing table.
type DropColumn struct {
	Table  string
	Column string
}

// AlterColumnUnique changes a column's uniqueness constraint.
type AlterColumnUnique struct {
	Table  string
	Column string
	Unique bool
}

// CreateIndex creates a plain index for a table column.
type CreateIndex struct {
	Table  string
	Column string
}

// DropIndex removes a plain index for a table column.
type DropIndex struct {
	Table  string
	Column string
}

// Migration groups the up/down steps for one generated migration file.
type Migration struct {
	App        string
	Name       string
	Up         []Step
	Down       []Step
	Reversible bool
}

func (CreateTable) isMigrationStep()       {}
func (DropTable) isMigrationStep()         {}
func (AddColumn) isMigrationStep()         {}
func (DropColumn) isMigrationStep()        {}
func (AlterColumnUnique) isMigrationStep() {}
func (CreateIndex) isMigrationStep()       {}
func (DropIndex) isMigrationStep()         {}
