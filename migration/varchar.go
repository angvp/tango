package migration

import (
	"fmt"

	"github.com/angvp/tango/model"
)

// validateColumn rejects a column whose Type and Length disagree: varchar
// needs a Length from 1 to model.MaxVarcharLength and no other type may carry
// one. Unknown legacy type tokens keep their silent TEXT fallback, but a
// varchar or length-bearing state is always a loud error.
func validateColumn(table string, c Column) error {
	return validateLength(table, c.Name, c.Type, c.Length)
}

func validateLength(table, column, columnType string, length int) error {
	switch {
	case columnType == "varchar" && (length < 1 || length > model.MaxVarcharLength):
		return fmt.Errorf("tango migration: %s.%s: varchar needs a length from 1 to %d, got %d", table, column, model.MaxVarcharLength, length)
	case columnType != "varchar" && length != 0:
		return fmt.Errorf("tango migration: %s.%s: a %s column cannot have a length (%d)", table, column, columnType, length)
	}
	return nil
}

// validateAlterLengths applies validateLength to both ends of a type change.
func validateAlterLengths(s AlterColumnType) error {
	if err := validateLength(s.Table, s.Column, s.From, s.FromLength); err != nil {
		return err
	}
	return validateLength(s.Table, s.Column, s.To, s.ToLength)
}
