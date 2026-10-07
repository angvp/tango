package cli

import (
	"fmt"
	"strings"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
)

// renameForm is the shape --rename takes, quoted in every malformed-value
// error.
const renameForm = "app.Model.Field=NewField"

// parseRename reads "app.Model.Field=NewField" into a Rename mapping. The
// model and fields may be written as Go names or as table and column names.
func parseRename(value string) (migration.Rename, error) {
	source, to, ok := strings.Cut(value, "=")
	parts := strings.Split(source, ".")
	if !ok || to == "" || strings.Contains(to, ".") || len(parts) < 2 || len(parts) > 3 || slicesContainEmpty(parts) {
		return migration.Rename{}, fmt.Errorf("--rename %q: want %s", value, renameForm)
	}
	if len(parts) == 2 {
		return migration.Rename{}, fmt.Errorf("--rename %q: renaming a model is not supported yet; want %s", value, renameForm)
	}
	return migration.Rename{
		App:    parts[0],
		Table:  db.ColumnName(parts[1]),
		Column: db.ColumnName(parts[2]),
		To:     db.ColumnName(to),
	}, nil
}

// parseRenames reads every --rename, reporting every malformed one at once.
func parseRenames(values []string) ([]migration.Rename, error) {
	renames := make([]migration.Rename, 0, len(values))
	var problems []string
	for _, value := range values {
		rename, err := parseRename(value)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		renames = append(renames, rename)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	return renames, nil
}
