package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0023_add_composite","up":[{"kind":"CreateCompositeIndex","table":"ledger","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"indexColumns":["code","qty"]},{"kind":"CreateCompositeIndex","table":"post","unique":true,"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"indexColumns":["slug","author_id"]},{"kind":"CreateCompositeIndex","table":"comment","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"indexColumns":["text","post_id"]}],"down":[{"kind":"DropCompositeIndex","table":"comment","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"indexColumns":["text","post_id"]},{"kind":"DropCompositeIndex","table":"post","unique":true,"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"indexColumns":["slug","author_id"]},{"kind":"DropCompositeIndex","table":"ledger","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"indexColumns":["code","qty"]}],"reversible":true}]
var M0023AddComposite = []migration.Migration{
	{App: "blog", Name: "0023_add_composite", Reversible: true,
		Up: []migration.Step{
			migration.CreateCompositeIndex{Table: "ledger", Columns: []string{"code", "qty"}},
			migration.CreateCompositeIndex{Table: "post", Columns: []string{"slug", "author_id"}, Unique: true},
			migration.CreateCompositeIndex{Table: "comment", Columns: []string{"text", "post_id"}},
		},
		Down: []migration.Step{
			migration.DropCompositeIndex{Table: "comment", Columns: []string{"text", "post_id"}},
			migration.DropCompositeIndex{Table: "post", Columns: []string{"slug", "author_id"}, Unique: true},
			migration.DropCompositeIndex{Table: "ledger", Columns: []string{"code", "qty"}},
		},
	},
}
