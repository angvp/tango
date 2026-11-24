package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0019_drop_default","up":[{"kind":"AlterColumnDefault","table":"ledger","column":"note","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"AlterColumnDefault","table":"ledger","column":"note","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"defaultKind":"string","defaultValue":"unknown"}],"reversible":true}]
var M0019DropDefault = []migration.Migration{
	{App: "blog", Name: "0019_drop_default", Reversible: true,
		Up: []migration.Step{
			migration.AlterColumnDefault{Table: "ledger", Column: "note"},
		},
		Down: []migration.Step{
			migration.AlterColumnDefault{Table: "ledger", Column: "note", DefaultKind: "string", DefaultValue: "unknown"},
		},
	},
}
