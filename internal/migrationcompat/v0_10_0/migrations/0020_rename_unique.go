package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0020_rename_unique","up":[{"kind":"RenameIndex","table":"ledger","column":"code","unique":true,"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"uniq_ledger_code","from":"idx_ledger_code"}],"down":[{"kind":"RenameIndex","table":"ledger","column":"code","unique":true,"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"idx_ledger_code","from":"uniq_ledger_code"}],"reversible":true}]
var M0020RenameUnique = []migration.Migration{
	{App: "blog", Name: "0020_rename_unique", Reversible: true,
		Up: []migration.Step{
			migration.RenameIndex{Table: "ledger", Column: "code", Unique: true, From: "idx_ledger_code", To: "uniq_ledger_code"},
		},
		Down: []migration.Step{
			migration.RenameIndex{Table: "ledger", Column: "code", Unique: true, From: "uniq_ledger_code", To: "idx_ledger_code"},
		},
	},
}
