package mailtest_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/angvp/tango/mail"
	"github.com/angvp/tango/mail/mailtest"
)

func TestSenderRecordsWhatItIsGiven(t *testing.T) {
	sender := &mailtest.Sender{}
	var wg sync.WaitGroup
	for _, to := range []string{"a@example.com", "b@example.com"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sender.Send(context.Background(), mail.Message{From: "noreply@example.com", To: to, Subject: "Hi", Text: "x"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := sender.Messages(); len(got) != 2 {
		t.Fatalf("Messages() = %+v, want two", got)
	}
}

func TestSenderRejectsAnInvalidMessageLikeARealSender(t *testing.T) {
	sender := &mailtest.Sender{}
	err := sender.Send(context.Background(), mail.Message{From: "noreply@example.com", To: "a@example.com\r\nBcc: x@example.com"})
	if !errors.Is(err, mail.ErrInvalidMessage) || len(sender.Messages()) != 0 {
		t.Fatalf("error = %v, messages = %d; want ErrInvalidMessage and none recorded", err, len(sender.Messages()))
	}
}

func TestSenderCanBeToldToFail(t *testing.T) {
	boom := errors.New("relay down")
	sender := &mailtest.Sender{Err: boom}
	if err := sender.Send(context.Background(), mail.Message{From: "noreply@example.com", To: "a@example.com"}); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
	if len(sender.Messages()) != 0 {
		t.Fatal("a failed send was recorded")
	}
}
