package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"accounts","name":"0006_auto_20261008201421","up":[{"kind":"AddColumn","table":"account","def":{"Name":"email_verified_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}},{"kind":"CreateTable","table":"account_token","columns":[{"Name":"id","Type":"integer","PrimaryKey":true,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"token_hash","Type":"text","PrimaryKey":false,"Unique":true,"Indexed":false,"References":"","Default":""},{"Name":"account_id","Type":"integer","PrimaryKey":false,"Unique":false,"Indexed":true,"References":"account","Default":""},{"Name":"purpose","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"address_hash","Type":"text","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},{"Name":"expires_at","Type":"timestamp","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}],"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"DropTable","table":"account_token","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}},{"kind":"DropColumn","table":"account","column":"email_verified_at","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0006Auto20261008201421 = []migration.Migration{
	{App: "accounts", Name: "0006_auto_20261008201421", Reversible: true,
		Up: []migration.Step{
			migration.AddColumn{Table: "account", Column: migration.Column{Name: "email_verified_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}},
			migration.CreateTable{Table: "account_token", Columns: []migration.Column{{Name: "id", Type: "integer", PrimaryKey: true, Unique: false, Indexed: false}, {Name: "token_hash", Type: "text", PrimaryKey: false, Unique: true, Indexed: false}, {Name: "account_id", Type: "integer", PrimaryKey: false, Unique: false, Indexed: true, References: "account"}, {Name: "purpose", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "address_hash", Type: "text", PrimaryKey: false, Unique: false, Indexed: false}, {Name: "expires_at", Type: "timestamp", PrimaryKey: false, Unique: false, Indexed: false}}},
		},
		Down: []migration.Step{
			migration.DropTable{Table: "account_token"},
			migration.DropColumn{Table: "account", Column: "email_verified_at"},
		},
	},
}
