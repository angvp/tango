package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"accounts","name":"0004_auto_20261007040937","up":[{"kind":"CreateTable","table":"account","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"email","Type":"text","PrimaryKey":false,"Unique":true,"Indexed":false,"References":"","Default":""},{"Name":"password_hash","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"active","Type":"boolean","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"created_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}},{"kind":"CreateTable","table":"account_session","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"token","Type":"text","PrimaryKey":false,"Unique":true,"Indexed":false,"References":"","Default":""},{"Name":"user_id","Type":"integer","PrimaryKey":false,"Unique":false,"Indexed":true,"References":"account","Default":""},{"Name":"expires_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"DropTable","table":"account","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}},{"kind":"DropTable","table":"account_session","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0004Auto20261007040937 = []migration.Migration{
	{App: "accounts", Name: "0004_auto_20261007040937", Reversible: true,
		Up: []migration.Step{
			migration.CreateTable{Table: "account", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "email", Type: "text", PrimaryKey: false, Unique: true, Indexed: false}, {Name: "password_hash", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "active", Type: "boolean", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "created_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}}},
			migration.CreateTable{Table: "account_session", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "token", Type: "text", PrimaryKey: false, Unique: true, Indexed: false}, {Name: "user_id", Type: "integer", PrimaryKey: false, Unique: false, Indexed: true, References: "account"}, {Name: "expires_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "account"},
			migration.DropTable{Table: "account_session"},
		},
	},
}
