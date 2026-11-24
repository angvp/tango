package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0025_drop_composite","up":[{"kind":"DropCompositeIndex","table":"post","unique":true,"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"indexColumns":["slug","author_id"]}],"down":[{"kind":"CreateCompositeIndex","table":"post","unique":true,"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"indexColumns":["slug","author_id"]}],"reversible":true}]
var M0025DropComposite = []migration.Migration{
	{App: "blog", Name: "0025_drop_composite", Reversible: true,
		Up: []migration.Step{
			migration.DropCompositeIndex{Table: "post", Columns: []string{"slug", "author_id"}, Unique: true},
		},
		Down: []migration.Step{
			migration.CreateCompositeIndex{Table: "post", Columns: []string{"slug", "author_id"}, Unique: true},
		},
	},
}
