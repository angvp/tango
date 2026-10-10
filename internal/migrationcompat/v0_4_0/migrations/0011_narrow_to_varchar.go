package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0011_narrow_to_varchar","up":[{"kind":"AlterColumnType","table":"comment","column":"text","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"varchar","from":"text","toLength":500}],"down":[{"kind":"AlterColumnType","table":"comment","column":"text","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"text","from":"varchar","fromLength":500}],"reversible":true}]
var M0011NarrowToVarchar = []migration.Migration{
	{App: "blog", Name: "0011_narrow_to_varchar", Reversible: true,
		Up: []migration.Step{
			migration.AlterColumnType{Table: "comment", Column: "text", From: "text", To: "varchar", ToLength: 500},
		},
		Down: []migration.Step{
			migration.AlterColumnType{Table: "comment", Column: "text", From: "varchar", FromLength: 500, To: "text"},
		},
	},
}
