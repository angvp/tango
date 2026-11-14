package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0004_drop_index","up":[{"kind":"DropIndex","table":"post","column":"body","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"CreateIndex","table":"post","column":"body","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0004DropIndex = []migration.Migration{
	{App: "blog", Name: "0004_drop_index", Reversible: true,
		Up: []migration.Step{
			migration.DropIndex{Table: "post", Column: "body"},
		},
		Down: []migration.Step{
			migration.CreateIndex{Table: "post", Column: "body"},
		},
	},
}
