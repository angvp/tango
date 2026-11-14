// Package schemaattr decides whether a column, a step or a model set uses the
// exact-schema attributes added in v0.10.0 (int4, naive, NOT NULL, typed
// defaults, explicit index names and the ALTER steps for them). The CLI uses
// it to give such steps header kinds an older reader refuses, and the root
// package uses it for the dump handshake. It imports only migration.
package schemaattr

import "github.com/angvp/tango/migration"

// HandshakeArg is the positional argument `tango makemigrations` passes after
// -tango-dump-models. An app whose models use an exact-schema attribute
// refuses to dump unless it is present, so a CLI older than v0.10.0, which
// would drop those attributes silently, cannot read the dump. The wire value
// is fixed.
const HandshakeArg = "tango-exact-schema-v1"

// ColumnUses reports whether c carries any exact-schema attribute.
func ColumnUses(c migration.Column) bool {
	return c.Width != 0 || c.Naive || c.NotNull || c.DefaultKind != "" ||
		c.DefaultValue != "" || c.UniqueName != "" || c.IndexName != ""
}

// StepUses reports whether step needs a reader from v0.10.0: a step that
// carries an exact-schema column or field, or one of the new step types.
func StepUses(step migration.Step) bool {
	switch s := step.(type) {
	case migration.CreateTable:
		for _, c := range s.Columns {
			if ColumnUses(c) {
				return true
			}
		}
	case migration.AddColumn:
		return ColumnUses(s.Column)
	case migration.CreateIndex:
		return s.Name != ""
	case migration.DropIndex:
		return s.Name != ""
	case migration.AlterColumnUnique:
		return s.Name != ""
	case migration.AlterColumnType:
		return s.FromWidth != 0 || s.ToWidth != 0 || s.FromNaive || s.ToNaive ||
			s.DefaultKind != "" || s.DefaultValue != ""
	case migration.AlterColumnNotNull, migration.AlterColumnDefault, migration.RenameIndex:
		return true
	}
	return false
}

// ModelField returns the first App.Struct.Field whose column uses an
// exact-schema attribute, or false when no model does.
func ModelField(models []migration.Model) (string, bool) {
	for _, m := range models {
		for _, c := range m.Columns {
			if !ColumnUses(c) {
				continue
			}
			field := c.Name
			if name, ok := m.Fields[c.Name]; ok {
				field = name
			}
			return m.App + "." + m.Struct + "." + field, true
		}
	}
	return "", false
}
