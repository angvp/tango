package admin

import (
	"context"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// uniqueViolationField returns the unique field whose index refused the
// write err reports. It reports false when err is not a unique violation,
// when the database does not name the constraint, or when more than one
// field could own it (a composite index), so the caller shows a form-level
// message instead.
func uniqueViolationField(meta model.ModelMeta, err error) (model.FieldMeta, bool) {
	key := uniqueViolationKey(err)
	if key == "" {
		return model.FieldMeta{}, false
	}
	table := db.ColumnName(meta.Name)
	for _, field := range meta.Fields {
		if !field.Unique {
			continue
		}
		// SQLite names the table and column. Postgres and MySQL name the
		// index, which the migration creates as uniq_<table>_<column>.
		column := db.ColumnName(field.Name)
		if key == table+"."+column || key == "uniq_"+table+"_"+column {
			return field, true
		}
	}
	return model.FieldMeta{}, false
}

// uniqueViolationKey extracts the constraint or column a unique violation
// names, from the driver's typed error where one exists and from the message
// otherwise. It returns "" when the key is absent or lists more than one
// column. The submitted value is never part of the key, so it is never shown.
func uniqueViolationKey(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		const marker = "for key '"
		i := strings.LastIndex(myErr.Message, marker)
		if i < 0 {
			return ""
		}
		return strings.TrimSuffix(myErr.Message[i+len(marker):], "'")
	}
	const marker = "UNIQUE constraint failed: "
	message := err.Error()
	i := strings.Index(message, marker)
	if i < 0 {
		return ""
	}
	key := message[i+len(marker):]
	if j := strings.Index(key, " ("); j >= 0 {
		key = key[:j]
	}
	if strings.Contains(key, ",") {
		return ""
	}
	return key
}

// uniqueFieldMessage is the message beside a field whose value another row
// already uses.
func uniqueFieldMessage(ctx context.Context, meta model.ModelMeta, field model.FieldMeta, opts adminregistry.Options) string {
	return i18n.T(ctx, "admin.error.unique_field", "Another %s already uses this %s.", displayNoun(meta, opts), strings.ToLower(fieldLabel(opts, field.Name)))
}

// fieldLabel is the label the form shows for a field: the host's Labels
// entry when it has one, else the humanized Go name.
func fieldLabel(opts adminregistry.Options, name string) string {
	if label, ok := opts.Labels[name]; ok {
		return label
	}
	return humanizeFieldName(name)
}

// displayNoun is the lowercase noun messages use for one row of the model.
func displayNoun(meta model.ModelMeta, opts adminregistry.Options) string {
	if opts.SingularName != "" {
		return strings.ToLower(opts.SingularName)
	}
	return strings.ToLower(humanizeFieldName(meta.Name))
}
