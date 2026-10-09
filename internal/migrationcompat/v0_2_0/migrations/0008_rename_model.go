package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0008_rename_model","up":[{"kind":"RenameTable","table":"author","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"writer"}],"down":[{"kind":"RenameTable","table":"writer","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"author"}],"reversible":true}]
var M0008RenameModel = []migration.Migration{
	{App: "blog", Name: "0008_rename_model", Reversible: true,
		Up: []migration.Step{
			migration.RenameTable{From: "author", To: "writer"},
		},
		Down: []migration.Step{
			migration.RenameTable{From: "writer", To: "author"},
		},
	},
}
