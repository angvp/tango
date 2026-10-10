package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"profiles","name":"0004_profile_preferences","up":[{"kind":"AddColumn","table":"profile","def":{"Name":"preferences","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"DropColumn","table":"profile","column":"preferences","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0004ProfilePreferences = []migration.Migration{
	{App: "profiles", Name: "0004_profile_preferences", Reversible: true,
		Up: []migration.Step{
			migration.AddColumn{Table: "profile", Column: migration.Column{Name: "preferences", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}},
		},
		Down: []migration.Step{
			migration.DropColumn{Table: "profile", Column: "preferences"},
		},
	},
}
