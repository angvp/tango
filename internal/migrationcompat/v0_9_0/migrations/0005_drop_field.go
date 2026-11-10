package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0005_drop_field","up":[{"kind":"DropColumn","table":"post","column":"published","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[],"reversible":false}]
var M0005DropField = []migration.Migration{
	{App: "blog", Name: "0005_drop_field", Reversible: false,
		Up: []migration.Step{
			migration.DropColumn{Table: "post", Column: "published"},
		},
		Down: []migration.Step{},
	},
}
