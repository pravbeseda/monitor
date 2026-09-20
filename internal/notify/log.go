package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/i18n"
)

// Log is the channel that needs no secret: a hub alerts into its own journal from the
// first start, before a bot exists. The line stays English whatever notify.locale says,
// because a log is diagnostic and ends up in a bug report (ADR 0008).
type Log struct {
	// Logger defaults to the process logger.
	Logger *slog.Logger
}

var _ evaluate.Notifier = Log{}

// Notify writes one line naming the subject, the level it reached and the values behind it.
func (l Log) Notify(_ context.Context, m evaluate.Message) error {
	l.logger().Warn("notification",
		"node", m.Node, "metric", m.Metric, "level", m.To.String(),
		"message", Render(i18n.For(i18n.English), m))
	return nil
}

// Digest writes the day's summary as one record, however many subjects it lists.
func (l Log) Digest(_ context.Context, at time.Time, entries []evaluate.Message, unwatched int) error {
	l.logger().Info("digest",
		"at", at, "subjects", len(entries), "unwatched", unwatched,
		"message", RenderDigest(i18n.For(i18n.English), entries, unwatched))
	return nil
}

func (l Log) logger() *slog.Logger {
	if l.Logger != nil {
		return l.Logger
	}
	return slog.Default()
}
