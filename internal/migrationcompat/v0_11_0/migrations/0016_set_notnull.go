package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0016_set_notnull","up":[{"kind":"AlterColumnNotNull","table":"ledger","column":"qty","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"notNull":true}],"down":[{"kind":"AlterColumnNotNull","table":"ledger","column":"qty","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0016SetNotnull = []migration.Migration{
	{App: "blog", Name: "0016_set_notnull", Reversible: true,
		Up: []migration.Step{
			migration.AlterColumnNotNull{Table: "ledger", Column: "qty", NotNull: true},
		},
		Down: []migration.Step{
			migration.AlterColumnNotNull{Table: "ledger", Column: "qty", NotNull: false},
		},
	},
}
