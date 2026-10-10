package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0012_widen_bound","up":[{"kind":"AlterColumnType","table":"post","column":"slug","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"varchar","from":"varchar","toLength":200,"fromLength":80}],"down":[],"reversible":false}]
var M0012WidenBound = []migration.Migration{
	{App: "blog", Name: "0012_widen_bound", Reversible: false,
		Up: []migration.Step{
			migration.AlterColumnType{Table: "post", Column: "slug", From: "varchar", FromLength: 80, To: "varchar", ToLength: 200},
		},
		Down: []migration.Step{},
	},
}
