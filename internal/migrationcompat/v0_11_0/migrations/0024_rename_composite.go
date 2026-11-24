package migrations

import "github.com/angvp/tango/migration"

// tango:migration-json [{"app":"blog","name":"0024_rename_composite","up":[{"kind":"RenameCompositeIndex","table":"comment","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"ix_comment_post_text","from":"idx_comment_text_post_id","indexColumns":["text","post_id"]}],"down":[{"kind":"RenameCompositeIndex","table":"comment","def":{"Name":"","Type":"","PrimaryKey":false,"Unique":false,"Indexed":false,"References":"","Default":""},"to":"idx_comment_text_post_id","from":"ix_comment_post_text","indexColumns":["text","post_id"]}],"reversible":true}]
var M0024RenameComposite = []migration.Migration{
	{App: "blog", Name: "0024_rename_composite", Reversible: true,
		Up: []migration.Step{
			migration.RenameCompositeIndex{Table: "comment", Columns: []string{"text", "post_id"}, From: "idx_comment_text_post_id", To: "ix_comment_post_text"},
		},
		Down: []migration.Step{
			migration.RenameCompositeIndex{Table: "comment", Columns: []string{"text", "post_id"}, From: "ix_comment_post_text", To: "idx_comment_text_post_id"},
		},
	},
}
