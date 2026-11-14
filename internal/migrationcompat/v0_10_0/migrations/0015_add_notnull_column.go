package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0015_add_notnull_column","up":[{"kind":"AddColumnV2","table":"ledger","def":{"Name":"tag","Type":"varchar","Length":20,"PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":"","NotNull":true,"DefaultKind":"string","DefaultValue":"x"}}],"down":[{"kind":"DropColumn","table":"ledger","column":"tag","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0015AddNotnullColumn = []migration.Migration{
	{App: "blog", Name: "0015_add_notnull_column", Reversible: true,
		Up: []migration.Step{
			migration.AddColumn{Table: "ledger", Column: migration.Column{Name: "tag", Type: "varchar", Length: 20, PrimaryKey: false, Unique: false, Indexed: false, NotNull: true, DefaultKind: "string", DefaultValue: "x"}},
		},
		Down: []migration.Step{
			migration.DropColumn{Table: "ledger", Column: "tag"},
		},
	},
}
