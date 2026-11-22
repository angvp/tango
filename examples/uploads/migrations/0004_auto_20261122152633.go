package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"documents","name":"0004_auto_20261122152633","up":[{"kind":"CreateTable","table":"folder","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"name","Type":"varchar","Length":100,"PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"cover","Type":"varchar","Length":32,"PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"DropTable","table":"folder","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0004Auto20261122152633 = []migration.Migration{
	{App: "documents", Name: "0004_auto_20261122152633", Reversible: true,
		Up: []migration.Step{
			migration.CreateTable{Table: "folder", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "name", Type: "varchar", Length: 100, PrimaryKey: false, Unique: false, Indexed: false}, {Name: "cover", Type: "varchar", Length: 32, PrimaryKey: false, Unique: false, Indexed: false}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "folder"},
		},
	},
}
