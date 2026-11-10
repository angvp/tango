package posts

import (
	"time"

	"github.com/angvp/tango/accounts"
)

type Post struct {
	ID        int64 `tango:"pk"`
	Title     string
	Body      string
	AccountID int64 `tango:"fk=Account,index"`
	// Account and Comments are read only with Query.Preload, and are never
	// written. They are not columns, so they need no migration. Their JSON
	// tags are "-": a response shapes them itself, so the password hash
	// cannot leave through a post.
	Account   *accounts.Account `tango:"belongs_to=AccountID" json:"-"`
	Comments  []Comment         `tango:"has_many=PostID" json:"-"`
	CreatedAt time.Time
}

type Comment struct {
	ID        int64 `tango:"pk"`
	PostID    int64 `tango:"fk=Post,index"`
	Author    string
	Body      string
	CreatedAt time.Time
}
