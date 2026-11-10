package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0003_constraints","up":[{"kind":"AlterColumnUnique","table":"author","column":"name","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}},{"kind":"AlterColumnUnique","table":"post","column":"title","unique":true,"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}},{"kind":"CreateIndex","table":"post","column":"body","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"down":[{"kind":"DropIndex","table":"post","column":"body","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}},{"kind":"AlterColumnUnique","table":"post","column":"title","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}},{"kind":"AlterColumnUnique","table":"author","column":"name","unique":true,"def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""}}],"reversible":true}]
var M0003Constraints = []migration.Migration{
	{App: "blog", Name: "0003_constraints", Reversible: true,
		Up: []migration.Step{
			migration.AlterColumnUnique{Table: "author", Column: "name", Unique: false},
			migration.AlterColumnUnique{Table: "post", Column: "title", Unique: true},
			migration.CreateIndex{Table: "post", Column: "body"},
		},
		Down: []migration.Step{
			migration.DropIndex{Table: "post", Column: "body"},
			migration.AlterColumnUnique{Table: "post", Column: "title", Unique: false},
			migration.AlterColumnUnique{Table: "author", Column: "name", Unique: true},
		},
	},
}
