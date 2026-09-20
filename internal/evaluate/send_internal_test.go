package evaluate

import (
	"context"
	"testing"
	"time"
)

// stuck is a channel that never answers on its own.
type stuck struct{ released chan struct{} }

func (s stuck) Notify(context.Context, Message) error {
	<-s.released
	return nil
}

func (s stuck) Digest(context.Context, time.Time, []Message, int) error {
	<-s.released
	return nil
}

// spec: evaluation.md#the-tick — a notifier that does not return cannot hold evaluation
// open: the send is abandoned and counted as a failure.
func TestASendThatNeverAnswersIsAbandoned(t *testing.T) {
	was := sendTimeout
	sendTimeout = 10 * time.Millisecond
	channel := stuck{released: make(chan struct{})}
	t.Cleanup(func() {
		sendTimeout = was
		close(channel.released)
	})

	evaluator := New(Options{Notifier: channel, Now: time.Now})
	for name, deliver := range map[string]func(context.Context) error{
		"a message": func(ctx context.Context) error { return channel.Notify(ctx, Message{}) },
		"a digest":  func(ctx context.Context) error { return channel.Digest(ctx, time.Time{}, nil, 0) },
	} {
		if err := evaluator.send(context.Background(), deliver); err == nil {
			t.Fatalf("%s the channel never answered was counted as a delivery", name)
		}
	}
}
