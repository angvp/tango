package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"documents","name":"0002_initial","up":[{"kind":"CreateTable","table":"document","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"owner_id","Type":"integer","PrimaryKey":false,"Unique":false,"Indexed":true,"References":"account","Default":""},{"Name":"key","Type":"varchar","Length":32,"PrimaryKey":false,"Unique":true,"Indexed":false,"References":"","Default":""},{"Name":"name","Type":"varchar","Length":255,"PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"content_type","Type":"varchar","Length":100,"PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"size","Type":"integer","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"sha256","Type":"varchar","Length":64,"PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"created_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"DropTable","table":"document","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0002Auto20261010045553 = []migration.Migration{
	{App: "documents", Name: "0002_initial", Reversible: true,
		Up: []migration.Step{
			migration.CreateTable{Table: "document", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "owner_id", Type: "integer", PrimaryKey: false, Unique: false, Indexed: true, References: "account"}, {Name: "key", Type: "varchar", Length: 32, PrimaryKey: false, Unique: true, Indexed: false}, {Name: "name", Type: "varchar", Length: 255, PrimaryKey: false, Unique: false, Indexed: false}, {Name: "content_type", Type: "varchar", Length: 100, PrimaryKey: false, Unique: false, Indexed: false}, {Name: "size", Type: "integer", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "sha256", Type: "varchar", Length: 64, PrimaryKey: false, Unique: false, Indexed: false}, {Name: "created_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "document"},
		},
	},
}
