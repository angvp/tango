package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json "W3siQXBwIjoibm90ZXMiLCJOYW1lIjoiMDAwMV9hdXRvIiwiVXAiOlt7IlR5cGUiOiJDcmVhdGVUYWJsZSIsIlRhYmxlIjoibm90ZSIsIkNvbHVtbnMiOlt7Ik5hbWUiOiJpZCIsIlR5cGUiOiJpbnRlZ2VyIiwiUHJpbWFyeUtleSI6dHJ1ZSwiVW5pcXVlIjpmYWxzZSwiSW5kZXhlZCI6ZmFsc2V9LHsiTmFtZSI6InRpdGxlIiwiVHlwZSI6InRleHQiLCJQcmltYXJ5S2V5IjpmYWxzZSwiVW5pcXVlIjpmYWxzZSwiSW5kZXhlZCI6ZmFsc2V9LHsiTmFtZSI6ImJvZHkiLCJUeXBlIjoidGV4dCIsIlByaW1hcnlLZXkiOmZhbHNlLCJVbmlxdWUiOmZhbHNlLCJJbmRleGVkIjpmYWxzZX0seyJOYW1lIjoicHJpdmF0ZSIsIlR5cGUiOiJib29sZWFuIiwiUHJpbWFyeUtleSI6ZmFsc2UsIlVuaXF1ZSI6ZmFsc2UsIkluZGV4ZWQiOmZhbHNlfV19XSwiRG93biI6W3siVHlwZSI6IkRyb3BUYWJsZSIsIlRhYmxlIjoibm90ZSJ9XSwiUmV2ZXJzaWJsZSI6dHJ1ZX1d"

var M0001Auto = migration.Migration{
	App:  "notes",
	Name: "0001_auto",
	Up: []migration.Step{
		migration.CreateTable{Table: "note", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "title", Type: "text"},
			{Name: "body", Type: "text"},
			{Name: "private", Type: "boolean"},
		}},
	},
	Down: []migration.Step{
		migration.DropTable{Table: "note"},
	},
	Reversible: true,
}
