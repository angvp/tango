package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0013_widen_to_text","up":[{"kind":"AlterColumnType","table":"post","column":"slug","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"text","from":"varchar","fromLength":200}],"down":[],"reversible":false}]
var M0013WidenToText = []migration.Migration{
	{App: "blog", Name: "0013_widen_to_text", Reversible: false,
		Up: []migration.Step{
			migration.AlterColumnType{Table: "post", Column: "slug", From: "varchar", FromLength: 200, To: "text"},
		},
		Down: []migration.Step{},
	},
}
