package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0006_drop_model","up":[{"kind":"DropTable","table":"draft","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[],"reversible":false}]
var M0006DropModel = []migration.Migration{
	{App: "blog", Name: "0006_drop_model", Reversible: false,
		Up: []migration.Step{
			migration.DropTable{Table: "draft"},
		},
		Down: []migration.Step{},
	},
}
