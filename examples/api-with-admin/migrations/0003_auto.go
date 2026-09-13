package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"admin","name":"0003_auto","up":[{"kind":"CreateTable","table":"admin_session","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false},{"Name":"token","Type":"text","PrimaryKey":false,"Unique":true,"Indexed":false},{"Name":"user_id","Type":"integer","PrimaryKey":false,"Unique":false,"Indexed":true},{"Name":"expires_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false}},{"kind":"CreateTable","table":"admin_user","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false},{"Name":"username","Type":"text","PrimaryKey":false,"Unique":true,"Indexed":false},{"Name":"password_hash","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false},{"Name":"active","Type":"boolean","PrimaryKey":false,"Unique":false,"Indexed":false},{"Name":"created_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false}}],"down":[{"kind":"DropTable","table":"admin_session","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false}},{"kind":"DropTable","table":"admin_user","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false}}],"reversible":true}]
var M0003Auto = []migration.Migration{
	{App: "admin", Name: "0003_auto", Reversible: true,
		Up: []migration.Step{
			migration.CreateTable{Table: "admin_session", Columns: []migration.Column{migration.Column{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, migration.Column{Name: "token", Type: "text", PrimaryKey: false, Unique: true, Indexed: false}, migration.Column{Name: "user_id", Type: "integer", PrimaryKey: false, Unique: false, Indexed: true}, migration.Column{Name: "expires_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}}},
			migration.CreateTable{Table: "admin_user", Columns: []migration.Column{migration.Column{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, migration.Column{Name: "username", Type: "text", PrimaryKey: false, Unique: true, Indexed: false}, migration.Column{Name: "password_hash", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, migration.Column{Name: "active", Type: "boolean", PrimaryKey: false, Unique: false, Indexed: false}, migration.Column{Name: "created_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "admin_session"},
			migration.DropTable{Table: "admin_user"},
		},
	},
}
