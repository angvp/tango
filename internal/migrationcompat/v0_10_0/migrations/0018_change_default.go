package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0018_change_default","up":[{"kind":"AlterColumnDefault","table":"ledger","column":"note","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"defaultKind":"string","defaultValue":"unknown"}],"down":[{"kind":"AlterColumnDefault","table":"ledger","column":"note","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"defaultKind":"string","defaultValue":"none"}],"reversible":true}]
var M0018ChangeDefault = []migration.Migration{
	{App: "blog", Name: "0018_change_default", Reversible: true,
		Up: []migration.Step{
			migration.AlterColumnDefault{Table: "ledger", Column: "note", DefaultKind: "string", DefaultValue: "unknown"},
		},
		Down: []migration.Step{
			migration.AlterColumnDefault{Table: "ledger", Column: "note", DefaultKind: "string", DefaultValue: "none"},
		},
	},
}
