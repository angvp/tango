package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0021_widen_int4","up":[{"kind":"AlterColumnTypeV2","table":"ledger","column":"id","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"integer","from":"integer","fromWidth":32},{"kind":"AlterColumnTypeV2","table":"ledger","column":"qty","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"integer","from":"integer","fromWidth":32}],"down":[],"reversible":false}]
var M0021WidenInt4 = []migration.Migration{
	{App: "blog", Name: "0021_widen_int4", Reversible: false,
		Up: []migration.Step{
			migration.AlterColumnType{Table: "ledger", Column: "id", From: "integer", To: "integer", FromWidth: 32},
			migration.AlterColumnType{Table: "ledger", Column: "qty", From: "integer", To: "integer", FromWidth: 32},
		},
		Down: []migration.Step{},
	},
}
