package mail

import (
	"context"
	"io"
)

// WriterSender returns a Sender that writes each message to w in the exact
// wire format an SMTP sender would send, for development. Choose it
// explicitly, for example with os.Stdout: nothing in tanGO falls back to it.
// Messages carry live links and attachment data, so don't point it at
// production logs.
func WriterSender(w io.Writer, opts ...Option) Sender {
	return &writerSender{w: w, options: newOptions(opts)}
}

type writerSender struct {
	w       io.Writer
	options options
}

func (s *writerSender) Send(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := encode(message, s.options)
	if err != nil {
		return err
	}
	_, err = s.w.Write(encoded.raw)
	return err
}
