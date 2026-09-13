package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"posts","name":"0004_auto","up":[{"kind":"AddColumn","table":"post","def":{"Name":"author_id","Type":"integer","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"author"}}],"down":[{"kind":"DropColumn","table":"post","column":"author_id","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}}],"reversible":true}]
var M0004Auto = []migration.Migration{
	{App: "posts", Name: "0004_auto", Reversible: true,
		Up: []migration.Step{
			migration.AddColumn{Table: "post", Column: migration.Column{Name: "author_id", Type: "integer", PrimaryKey: false, Unique: false, Indexed: false, References: "author"}},
		},
		Down: []migration.Step{
			migration.DropColumn{Table: "post", Column: "author_id"},
		},
	},
}
