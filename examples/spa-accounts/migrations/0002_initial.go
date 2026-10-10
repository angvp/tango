package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"profiles","name":"0002_initial","up":[{"kind":"CreateTable","table":"profile","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"account_id","Type":"integer","PrimaryKey":false,"Unique":true,"Indexed":false,"References":"account","Default":""},{"Name":"username","Type":"varchar","Length":30,"PrimaryKey":false,"Unique":true,"Indexed":false,"References":"","Default":""},{"Name":"display_name","Type":"varchar","Length":60,"PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"bio","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"DropTable","table":"profile","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0002Initial = []migration.Migration{
	{App: "profiles", Name: "0002_initial", Reversible: true,
		Up: []migration.Step{
			migration.CreateTable{Table: "profile", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "account_id", Type: "integer", PrimaryKey: false, Unique: true, Indexed: false, References: "account"}, {Name: "username", Type: "varchar", Length: 30, PrimaryKey: false, Unique: true, Indexed: false}, {Name: "display_name", Type: "varchar", Length: 60, PrimaryKey: false, Unique: false, Indexed: false}, {Name: "bio", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "profile"},
		},
	},
}
