package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0002_add_fields","up":[{"kind":"AddColumn","table":"post","def":{"Name":"published","Type":"boolean","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}},{"kind":"AddColumn","table":"post","def":{"Name":"rating","Type":"real","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"DropColumn","table":"post","column":"rating","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}},{"kind":"DropColumn","table":"post","column":"published","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0002AddFields = []migration.Migration{
	{App: "blog", Name: "0002_add_fields", Reversible: true,
		Up: []migration.Step{
			migration.AddColumn{Table: "post", Column: migration.Column{Name: "published", Type: "boolean", PrimaryKey: false, Unique: false, Indexed: false}},
			migration.AddColumn{Table: "post", Column: migration.Column{Name: "rating", Type: "real", PrimaryKey: false, Unique: false, Indexed: false}},
		},
		Down: []migration.Step{
			migration.DropColumn{Table: "post", Column: "rating"},
			migration.DropColumn{Table: "post", Column: "published"},
		},
	},
}
