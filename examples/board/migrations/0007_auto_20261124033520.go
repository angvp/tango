package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"posts","name":"0007_auto_20261124033520","up":[{"kind":"CreateCompositeIndex","table":"comment","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"indexColumns":["post_id","created_at"]}],"down":[{"kind":"DropCompositeIndex","table":"comment","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"indexColumns":["post_id","created_at"]}],"reversible":true}]
var M0007Auto20261124033520 = []migration.Migration{
	{App: "posts", Name: "0007_auto_20261124033520", Reversible: true,
		Up: []migration.Step{
			migration.CreateCompositeIndex{Table: "comment", Columns: []string{"post_id", "created_at"}},
		},
		Down: []migration.Step{
			migration.DropCompositeIndex{Table: "comment", Columns: []string{"post_id", "created_at"}},
		},
	},
}
