package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"posts","name":"0002_auto_20261007040652","up":[{"kind":"CreateTable","table":"post","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"title","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"body","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"created_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"DropTable","table":"post","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0002Auto20261007040652 = []migration.Migration{
	{App: "posts", Name: "0002_auto_20261007040652", Reversible: true,
		Up: []migration.Step{
			migration.CreateTable{Table: "post", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "title", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "body", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "created_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "post"},
		},
	},
}
