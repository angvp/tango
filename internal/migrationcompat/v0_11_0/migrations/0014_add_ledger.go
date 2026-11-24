package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0014_add_ledger","up":[{"kind":"CreateTableV2","table":"ledger","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":"","Default":"","Width":32},{"Name":"code","Type":"varchar","Length":20,"PrimaryKey":false,"Unique":true,"Indexed":false,"References":"","Default":"","UniqueName":"idx_ledger_code"},{"Name":"note","Type":"varchar","Length":40,"PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":"","DefaultKind":"string","DefaultValue":"none"},{"Name":"at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":"","Naive":true,"NotNull":true},{"Name":"qty","Type":"integer","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":"","Width":32}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"DropTable","table":"ledger","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0014AddLedger = []migration.Migration{
	{App: "blog", Name: "0014_add_ledger", Reversible: true,
		Up: []migration.Step{
			migration.CreateTable{Table: "ledger", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false, Width: 32}, {Name: "code", Type: "varchar", Length: 20, PrimaryKey: false, Unique: true, Indexed: false, UniqueName: "idx_ledger_code"}, {Name: "note", Type: "varchar", Length: 40, PrimaryKey: false, Unique: false, Indexed: false, DefaultKind: "string", DefaultValue: "none"}, {Name: "at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false, Naive: true, NotNull: true}, {Name: "qty", Type: "integer", PrimaryKey: false, Unique: false, Indexed: false, Width: 32}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "ledger"},
		},
	},
}
