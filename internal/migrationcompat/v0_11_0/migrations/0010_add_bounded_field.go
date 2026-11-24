package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0010_add_bounded_field","up":[{"kind":"AddColumn","table":"post","def":{"Name":"slug","Type":"varchar","Length":80,"PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"DropColumn","table":"post","column":"slug","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0010AddBoundedField = []migration.Migration{
	{App: "blog", Name: "0010_add_bounded_field", Reversible: true,
		Up: []migration.Step{
			migration.AddColumn{Table: "post", Column: migration.Column{Name: "slug", Type: "varchar", Length: 80, PrimaryKey: false, Unique: false, Indexed: false}},
		},
		Down: []migration.Step{
			migration.DropColumn{Table: "post", Column: "slug"},
		},
	},
}
