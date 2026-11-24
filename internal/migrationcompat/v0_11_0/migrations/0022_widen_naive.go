package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0022_widen_naive","up":[{"kind":"AlterColumnTypeV2","table":"ledger","column":"at","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"timestamp","from":"timestamp","fromNaive":true}],"down":[],"reversible":false}]
var M0022WidenNaive = []migration.Migration{
	{App: "blog", Name: "0022_widen_naive", Reversible: false,
		Up: []migration.Step{
			migration.AlterColumnType{Table: "ledger", Column: "at", From: "timestamp", To: "timestamp", FromNaive: true},
		},
		Down: []migration.Step{},
	},
}
