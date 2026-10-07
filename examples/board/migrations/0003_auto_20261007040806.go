package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"posts","name":"0003_auto_20261007040806","up":[{"kind":"CreateTable","table":"comment","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"post_id","Type":"integer","PrimaryKey":false,"Unique":false,"Indexed":true,"References":"post","Default":""},{"Name":"author","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"body","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"created_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"DropTable","table":"comment","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0003Auto20261007040806 = []migration.Migration{
	{App: "posts", Name: "0003_auto_20261007040806", Reversible: true,
		Up: []migration.Step{
			migration.CreateTable{Table: "comment", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "post_id", Type: "integer", PrimaryKey: false, Unique: false, Indexed: true, References: "post"}, {Name: "author", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "body", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "created_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "comment"},
		},
	},
}
