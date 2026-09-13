package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"greetings","name":"0001_auto","up":[{"kind":"CreateTable","table":"greeting","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":""},{"Name":"name","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""},{"Name":"created_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}}],"down":[{"kind":"DropTable","table":"greeting","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}}],"reversible":true}]
var M0001Auto = []migration.Migration{
	{App: "greetings", Name: "0001_auto", Reversible: true,
		Up: []migration.Step{
			migration.CreateTable{Table: "greeting", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "name", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "created_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "greeting"},
		},
	},
}
