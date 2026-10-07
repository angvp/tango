package posts

import "time"

type Post struct {
	ID        int64 `tango:"pk"`
	Title     string
	Body      string
	AccountID int64 `tango:"fk=Account,index"`
	CreatedAt time.Time
}

type Comment struct {
	ID        int64 `tango:"pk"`
	PostID    int64 `tango:"fk=Post,index"`
	Author    string
	Body      string
	CreatedAt time.Time
}
