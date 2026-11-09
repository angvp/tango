package browsersmoke_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestNamedActionNamesStalledAction checks the labelled wrapper without a
// browser: a stalled action under a short deadline must fail with its label.
func TestNamedActionNamesStalledAction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	stalled := func(ctx context.Context, _ *chromedp.Target) (chromedp.Void, error) {
		<-ctx.Done()
		return chromedp.Void{}, ctx.Err()
	}

	_, err := named("WaitVisible #form-summary", stalled)(ctx, nil)
	if err == nil {
		t.Fatal("a stalled action returned no error")
	}
	if want := `action "WaitVisible #form-summary"`; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the action; want it to contain %q", err, want)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error %q does not wrap context.DeadlineExceeded", err)
	}
}

// TestNamedActionPassesSuccessThrough checks that a successful action is
// unchanged by the wrapper: no error, and it runs exactly once.
func TestNamedActionPassesSuccessThrough(t *testing.T) {
	runs := 0
	ok := func(context.Context, *chromedp.Target) (chromedp.Void, error) {
		runs++
		return chromedp.Void{}, nil
	}
	if _, err := named("Sleep", ok)(context.Background(), nil); err != nil {
		t.Fatalf("a successful action returned %v", err)
	}
	if runs != 1 {
		t.Fatalf("the action ran %d times, want 1", runs)
	}
}
