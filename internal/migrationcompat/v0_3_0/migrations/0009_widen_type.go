package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0009_widen_type","up":[{"kind":"AlterColumnType","table":"post","column":"score","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"real","from":"integer"}],"down":[],"reversible":false}]
var M0009WidenType = []migration.Migration{
	{App: "blog", Name: "0009_widen_type", Reversible: false,
		Up: []migration.Step{
			migration.AlterColumnType{Table: "post", Column: "score", From: "integer", To: "real"},
		},
		Down: []migration.Step{},
	},
}
