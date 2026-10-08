package mail_test

import (
	"io"
	"mime/quotedprintable"
)

func qpReader(r io.Reader) io.Reader { return quotedprintable.NewReader(r) }
