package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0017_drop_notnull","up":[{"kind":"AlterColumnNotNull","table":"ledger","column":"qty","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"AlterColumnNotNull","table":"ledger","column":"qty","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"notNull":true}],"reversible":true}]
var M0017DropNotnull = []migration.Migration{
	{App: "blog", Name: "0017_drop_notnull", Reversible: true,
		Up: []migration.Step{
			migration.AlterColumnNotNull{Table: "ledger", Column: "qty", NotNull: false},
		},
		Down: []migration.Step{
			migration.AlterColumnNotNull{Table: "ledger", Column: "qty", NotNull: true},
		},
	},
}
