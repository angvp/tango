package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0007_rename_field","up":[{"kind":"RenameColumn","table":"post","column":"title","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"heading"}],"down":[{"kind":"RenameColumn","table":"post","column":"heading","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"title"}],"reversible":true}]
var M0007RenameField = []migration.Migration{
	{App: "blog", Name: "0007_rename_field", Reversible: true,
		Up: []migration.Step{
			migration.RenameColumn{Table: "post", From: "title", To: "heading"},
		},
		Down: []migration.Step{
			migration.RenameColumn{Table: "post", From: "heading", To: "title"},
		},
	},
}
