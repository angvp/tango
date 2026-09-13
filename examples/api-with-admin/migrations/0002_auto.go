package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"authors","name":"0002_auto","up":[{"kind":"CreateTable","table":"author","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":""},{"Name":"name","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""},{"Name":"email","Type":"text","PrimaryKey":false,"Unique":true,"Indexed":false,"References":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}}],"down":[{"kind":"DropTable","table":"author","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}}],"reversible":true}]
var M0002Auto = []migration.Migration{
	{App: "authors", Name: "0002_auto", Reversible: true,
		Up: []migration.Step{
			migration.CreateTable{Table: "author", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "name", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "email", Type: "text", PrimaryKey: false, Unique: true, Indexed: false}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "author"},
		},
	},
}
