package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"admin","name":"0003_auto","up":[{"kind":"CreateTable","table":"admin_session","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":""},{"Name":"token","Type":"text","PrimaryKey":false,"Unique":true,"Indexed":false,"References":""},{"Name":"user_id","Type":"integer","PrimaryKey":false,"Unique":false,"Indexed":true,"References":""},{"Name":"expires_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}},{"kind":"CreateTable","table":"admin_user","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":""},{"Name":"username","Type":"text","PrimaryKey":false,"Unique":true,"Indexed":false,"References":""},{"Name":"password_hash","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""},{"Name":"active","Type":"boolean","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""},{"Name":"created_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}}],"down":[{"kind":"DropTable","table":"admin_session","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}},{"kind":"DropTable","table":"admin_user","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":""}}],"reversible":true}]
var M0003Auto = []migration.Migration{
	{App: "admin", Name: "0003_auto", Reversible: true,
		Up: []migration.Step{
			migration.CreateTable{Table: "admin_session", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "token", Type: "text", PrimaryKey: false, Unique: true, Indexed: false}, {Name: "user_id", Type: "integer", PrimaryKey: false, Unique: false, Indexed: true}, {Name: "expires_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}}},
			migration.CreateTable{Table: "admin_user", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "username", Type: "text", PrimaryKey: false, Unique: true, Indexed: false}, {Name: "password_hash", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "active", Type: "boolean", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "created_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "admin_session"},
			migration.DropTable{Table: "admin_user"},
		},
	},
}
