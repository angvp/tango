package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0026_rename_column_of_composite","up":[{"kind":"RenameColumn","table":"ledger","column":"qty","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"quantity"},{"kind":"RenameCompositeIndex","table":"ledger","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"idx_ledger_code_quantity","from":"idx_ledger_code_qty","indexColumns":["code","quantity"]}],"down":[{"kind":"RenameCompositeIndex","table":"ledger","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"idx_ledger_code_qty","from":"idx_ledger_code_quantity","indexColumns":["code","quantity"]},{"kind":"RenameColumn","table":"ledger","column":"quantity","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"qty"}],"reversible":true}]
var M0026RenameColumnOfComposite = []migration.Migration{
	{App: "blog", Name: "0026_rename_column_of_composite", Reversible: true,
		Up: []migration.Step{
			migration.RenameColumn{Table: "ledger", From: "qty", To: "quantity"},
			migration.RenameCompositeIndex{Table: "ledger", Columns: []string{"code", "quantity"}, From: "idx_ledger_code_qty", To: "idx_ledger_code_quantity"},
		},
		Down: []migration.Step{
			migration.RenameCompositeIndex{Table: "ledger", Columns: []string{"code", "quantity"}, From: "idx_ledger_code_quantity", To: "idx_ledger_code_qty"},
			migration.RenameColumn{Table: "ledger", From: "quantity", To: "qty"},
		},
	},
}
