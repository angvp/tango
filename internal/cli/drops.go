package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
)

// stringList is a flag that may be given several times, keeping each value.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

// schemaItem names a model's table, or one column of it, the way migration
// history knows it: app, table and column names, column empty for a model.
type schemaItem struct {
	app, table, column string
}

func (i schemaItem) String() string {
	if i.column == "" {
		return i.app + "." + i.table
	}
	return i.app + "." + i.table + "." + i.column
}

// parseSchemaItem reads "app.Model" or "app.Model.Field". The model and
// field may be written as Go names or as table and column names: both map
// to the same item.
func parseSchemaItem(flagName, value string) (schemaItem, error) {
	parts := strings.Split(value, ".")
	if (len(parts) != 2 && len(parts) != 3) || slicesContainEmpty(parts) {
		return schemaItem{}, fmt.Errorf("%s %q: want app.Model.Field or app.Model", flagName, value)
	}
	item := schemaItem{app: parts[0], table: db.ColumnName(parts[1])}
	if len(parts) == 3 {
		item.column = db.ColumnName(parts[2])
	}
	return item, nil
}

func slicesContainEmpty(parts []string) bool {
	for _, part := range parts {
		if part == "" {
			return true
		}
	}
	return false
}

// plannedDrops lists every model and field changes would drop, in order.
func plannedDrops(changes []migration.Migration, state migration.SchemaState) []schemaItem {
	var drops []schemaItem
	for _, change := range changes {
		for _, step := range change.Up {
			switch s := step.(type) {
			case migration.DropColumn:
				drops = append(drops, schemaItem{app: state.Tables[s.Table].App, table: s.Table, column: s.Column})
			case migration.DropTable:
				drops = append(drops, schemaItem{app: state.Tables[s.Table].App, table: s.Table})
			}
		}
	}
	return drops
}

// checkDrops fails unless every model and field changes would drop is
// authorised by exactly one --allow-drop, and every --allow-drop authorises
// one of them. It reports every problem at once.
func checkDrops(changes []migration.Migration, state migration.SchemaState, allowDrops []string) error {
	var problems []string
	authorised := make(map[schemaItem]bool)
	var authorisedOrder []schemaItem
	values := make(map[schemaItem]string)
	for _, value := range allowDrops {
		item, err := parseSchemaItem("--allow-drop", value)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if authorised[item] {
			problems = append(problems, fmt.Sprintf("--allow-drop %q authorises %s more than once", value, item))
			continue
		}
		authorised[item] = true
		authorisedOrder = append(authorisedOrder, item)
		values[item] = value
	}

	planned := make(map[schemaItem]bool)
	var unauthorised []schemaItem
	for _, drop := range plannedDrops(changes, state) {
		planned[drop] = true
		if !authorised[drop] {
			unauthorised = append(unauthorised, drop)
		}
	}
	for _, item := range authorisedOrder {
		if !planned[item] {
			problems = append(problems, fmt.Sprintf("--allow-drop %q: %s is not dropped by this change", values[item], item))
		}
	}
	if len(unauthorised) > 0 {
		problems = append(problems, refuseDrops(unauthorised))
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "\n"))
}

// refuseDrops explains the drops nothing authorised, with the commands that
// keep the data under a new name or drop it.
func refuseDrops(drops []schemaItem) string {
	var b strings.Builder
	b.WriteString("refusing to drop data no --allow-drop authorises:\n")
	allowDrops := make([]string, len(drops))
	for i, drop := range drops {
		kind, newName := "model", "<NewModel>"
		if drop.column != "" {
			kind, newName = "field", "<NewField>"
		}
		fmt.Fprintf(&b, "  %s %s\n", kind, drop)
		fmt.Fprintf(&b, "    renamed? keep its data: --rename %s=%s\n", drop, newName)
		allowDrops[i] = "--allow-drop " + drop.String()
	}
	fmt.Fprintf(&b, "to drop them and their data: tango makemigrations %s", strings.Join(allowDrops, " "))
	return b.String()
}
