package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"posts","name":"0005_auto_20261007040937","up":[{"kind":"AddColumn","table":"post","def":{"Name":"account_id","Type":"integer","PrimaryKey":false,"Unique":false,"Indexed":true,"References":"account","Default":""}}],"down":[{"kind":"DropColumn","table":"post","column":"account_id","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0005Auto20261007040937 = []migration.Migration{
	{App: "posts", Name: "0005_auto_20261007040937", Reversible: true,
		Up: []migration.Step{
			migration.AddColumn{Table: "post", Column: migration.Column{Name: "account_id", Type: "integer", PrimaryKey: false, Unique: false, Indexed: true, References: "account"}},
		},
		Down: []migration.Step{
			migration.DropColumn{Table: "post", Column: "account_id"},
		},
	},
}
