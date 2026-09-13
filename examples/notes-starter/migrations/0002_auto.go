package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app": "admin", "name": "0002_auto", "up": [{"kind": "CreateTable", "table": "admin_user", "columns": [{"Name": "id", "Type": "integer", "PrimaryKey": true, "Unique": false, "Indexed": false}, {"Name": "username", "Type": "text", "PrimaryKey": false, "Unique": true, "Indexed": false}, {"Name": "password_hash", "Type": "text", "PrimaryKey": false, "Unique": false, "Indexed": false}, {"Name": "active", "Type": "boolean", "PrimaryKey": false, "Unique": false, "Indexed": false}, {"Name": "created_at", "Type": "timestamp", "PrimaryKey": false, "Unique": false, "Indexed": false}]}, {"kind": "CreateTable", "table": "admin_session", "columns": [{"Name": "id", "Type": "integer", "PrimaryKey": true, "Unique": false, "Indexed": false}, {"Name": "token", "Type": "text", "PrimaryKey": false, "Unique": true, "Indexed": false}, {"Name": "user_id", "Type": "integer", "PrimaryKey": false, "Unique": false, "Indexed": true}, {"Name": "expires_at", "Type": "timestamp", "PrimaryKey": false, "Unique": false, "Indexed": false}]}], "down": [{"kind": "DropTable", "table": "admin_session"}, {"kind": "DropTable", "table": "admin_user"}], "reversible": true}]

var M0002Auto = migration.Migration{
	App:  "admin",
	Name: "0002_auto",
	Up: []migration.Step{
		migration.CreateTable{Table: "admin_user", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "username", Type: "text", Unique: true},
			{Name: "password_hash", Type: "text"},
			{Name: "active", Type: "boolean"},
			{Name: "created_at", Type: "timestamp"},
		}},
		migration.CreateTable{Table: "admin_session", Columns: []migration.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "token", Type: "text", Unique: true},
			{Name: "user_id", Type: "integer", Indexed: true},
			{Name: "expires_at", Type: "timestamp"},
		}},
	},
	Down: []migration.Step{
		migration.DropTable{Table: "admin_session"},
		migration.DropTable{Table: "admin_user"},
	},
	Reversible: true,
}
