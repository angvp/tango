package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"admin","name":"0005_add_admin_staff_and_superuser","up":[{"kind":"AddColumn","table":"admin_user","def":{"Name":"is_staff","Type":"boolean","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":"TRUE"}},{"kind":"AddColumn","table":"admin_user","def":{"Name":"is_superuser","Type":"boolean","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":"TRUE"}}],"down":[{"kind":"DropColumn","table":"admin_user","column":"is_staff","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}},{"kind":"DropColumn","table":"admin_user","column":"is_superuser","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
// is_staff and is_superuser each carry a Default of "TRUE" (a raw SQL
// literal, not a typed Go value — see migration.Column.Default) so this
// AddColumn backfills every pre-existing admin_user row to true instead of
// leaving it NULL. Milestone 18 hand-authored this Default; makemigrations'
// model-tag diffing never produces one on its own.
var M0005AddAdminStaffAndSuperuser = []migration.Migration{
	{App: "admin", Name: "0005_add_admin_staff_and_superuser", Reversible: true,
		Up: []migration.Step{
			migration.AddColumn{Table: "admin_user", Column: migration.Column{Name: "is_staff", Type: "boolean", PrimaryKey: false, Unique: false, Indexed: false, Default: "TRUE"}},
			migration.AddColumn{Table: "admin_user", Column: migration.Column{Name: "is_superuser", Type: "boolean", PrimaryKey: false, Unique: false, Indexed: false, Default: "TRUE"}},
		},
		Down: []migration.Step{
			migration.DropColumn{Table: "admin_user", Column: "is_staff"},
			migration.DropColumn{Table: "admin_user", Column: "is_superuser"},
		},
	},
}
