package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"posts","name":"0001_auto","up":[{"kind":"CreateTable","table":"post","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":""},{"Name":"title","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""},{"Name":"body","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""},{"Name":"created_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}}],"down":[{"kind":"DropTable","table":"post","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}}],"reversible":true}]
var M0001Auto = []migration.Migration{
	{App: "posts", Name: "0001_auto", Reversible: true,
		Up: []migration.Step{
			migration.CreateTable{Table: "post", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "title", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "body", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "created_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "post"},
		},
	},
}
