package evaluate_test

import (
	"context"
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/evaluate"
	"github.com/pravbeseda/monitor/internal/storage"
)

// The digest goes out at 09:00 UTC, so the tick at 12:00 is always past that day's hour.
var (
	schedule   = evaluate.Schedule{Hour: 9, Location: time.UTC}
	occurrence = time.Date(2026, time.August, 30, 9, 0, 0, 0, time.UTC)
	yesterday  = occurrence.AddDate(0, 0, -1)
)

// digesting builds an evaluator whose digest window is open: the hub has been up since
// yesterday, so the most recent 09:00 is later than where the window began.
func digesting(store evaluate.Store, channel evaluate.Notifier, at time.Time, targets ...evaluate.Target) *evaluate.Evaluator {
	return evaluate.New(evaluate.Options{
		Store: store, Notifier: channel, Targets: targets,
		Digest:  schedule,
		Started: yesterday.Add(-time.Hour),
		Now:     func() time.Time { return at },
	})
}

func mark(t *testing.T, db *storage.SQLite, at time.Time) {
	t.Helper()
	if err := db.SetLastDigestAt(context.Background(), at); err != nil {
		t.Fatalf("SetLastDigestAt: %v", err)
	}
}

func lastDigest(t *testing.T, db *storage.SQLite) (time.Time, bool) {
	t.Helper()
	at, ok, err := db.LastDigestAt(context.Background())
	if err != nil {
		t.Fatalf("LastDigestAt: %v", err)
	}
	return at, ok
}

// warned puts one volume into warning on an earlier tick, so the digest window carries a
// transition the current pass did not write.
func warned(t *testing.T, db *storage.SQLite, at time.Time, mount string) {
	t.Helper()
	collect(t, db, at, volume(mount), gb(9))
	pass(t, evaluator(db, at, watching(t)))
}

// spec: evaluation.md#digest — the tick that crosses `digest.at` sends one message listing
// every warning transition since the previous digest and every subject currently in warning.
func TestTheDigestCarriesTransitionsAndStandingWarnings(t *testing.T) {
	db := open(t)
	warned(t, db, yesterday.Add(time.Hour), "/data")
	warned(t, db, yesterday.Add(2*time.Hour), "/srv")

	// /data recovers before the digest, so its last move of the window is that recovery.
	recovered := yesterday.Add(3 * time.Hour)
	collect(t, db, recovered, volume("/data"), gb(40))
	pass(t, evaluator(db, recovered, watching(t)))

	channel := &recorder{}
	at := occurrence.Add(3 * time.Hour)
	collect(t, db, at, volume("/data"), gb(40))
	collect(t, db, at, volume("/srv"), gb(9))
	pass(t, digesting(db, channel, at, watching(t)))

	summaries := channel.summaries()
	if len(summaries) != 1 || len(summaries[0]) != 2 {
		t.Fatalf("the digest went out as %d messages carrying %v", len(summaries), summaries)
	}
	if got := summaries[0][0]; got.Labels["mount"] != "/data" || got.To != evaluate.OK {
		t.Fatalf("the first entry is %q at %v, want the recovered volume's transition",
			got.Labels["mount"], got.To)
	}
	if got := summaries[0][1]; got.Labels["mount"] != "/srv" || got.To != evaluate.Warning {
		t.Fatalf("the second entry is %q at %v, want the standing warning",
			got.Labels["mount"], got.To)
	}
}

// spec: evaluation.md#digest — a database that has never digested replays no history: the
// window begins at the hub's first start.
func TestADatabaseThatHasNeverDigestedReplaysNothing(t *testing.T) {
	db := open(t)
	warned(t, db, yesterday.Add(time.Hour), "/data")

	channel := &recorder{}
	fresh := evaluate.New(evaluate.Options{
		Store: db, Notifier: channel, Targets: []evaluate.Target{watching(t)},
		Digest:  schedule,
		Started: occurrence.Add(time.Hour),
		Now:     func() time.Time { return occurrence.Add(2 * time.Hour) },
	})
	pass(t, fresh)

	if got := channel.summaries(); len(got) != 0 {
		t.Fatalf("a first start digested %v", got)
	}
}

// spec: evaluation.md#digest — a subject that both transitioned to warning and is still in
// warning is listed once.
func TestASubjectStillInWarningIsListedOnce(t *testing.T) {
	db := open(t)
	warned(t, db, yesterday.Add(time.Hour), "/data")

	channel := &recorder{}
	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/data"), gb(9))
	pass(t, digesting(db, channel, at, watching(t)))

	summaries := channel.summaries()
	if len(summaries) != 1 || len(summaries[0]) != 1 {
		t.Fatalf("a subject listed twice: %v", summaries)
	}
}

// spec: evaluation.md#digest — a warning transition written by the same tick that sends the
// digest is included.
func TestATransitionOfTheDigestTickIsIncluded(t *testing.T) {
	db := open(t)
	mark(t, db, yesterday)

	channel := &recorder{}
	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/data"), gb(9))
	pass(t, digesting(db, channel, at, watching(t)))

	summaries := channel.summaries()
	if len(summaries) != 1 || len(summaries[0]) != 1 || summaries[0][0].From != evaluate.OK {
		t.Fatalf("the tick's own transition produced %v", summaries)
	}
}

// spec: evaluation.md#digest — nothing to say means no message, and the window still moves
// on: silence while all is well.
func TestAnEmptyDigestSendsNothingAndStillCloses(t *testing.T) {
	db := open(t)
	mark(t, db, yesterday)
	channel := &recorder{}
	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/"), gb(40))
	pass(t, digesting(db, channel, at, watching(t)))

	if got := channel.summaries(); len(got) != 0 {
		t.Fatalf("a quiet day digested %v", got)
	}
	if got, ok := lastDigest(t, db); !ok || !got.Equal(at) {
		t.Fatalf("the window closed at %v, want the tick %v that read it", got, at)
	}
}

// spec: evaluation.md#digest — a subject in critical and nothing in warning is no digest:
// the critical was reported instantly.
func TestACriticalAloneIsNoDigest(t *testing.T) {
	db := open(t)
	mark(t, db, yesterday)
	channel := &recorder{}
	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/"), gb(3))
	pass(t, digesting(db, channel, at, watching(t)))

	if got := channel.summaries(); len(got) != 0 {
		t.Fatalf("a critical produced the digest %v", got)
	}
}

// spec: evaluation.md#digest — several warnings on several nodes make one message, its
// entries ordered by node name then by mount.
func TestOneDigestForEveryNodeOrderedByNodeThenMount(t *testing.T) {
	db := open(t)
	mark(t, db, yesterday)
	at := occurrence.Add(time.Hour)

	first := watching(t)
	second := watching(t)
	second.Node = "laptop-a"
	for _, mount := range []string{"/srv", "/data"} {
		collect(t, db, at, volume(mount), gb(9))
	}
	if err := db.SaveIngest(context.Background(), storage.Ingest{
		Node: "laptop-a", AgentVersion: "test", ConfigVersion: "test", ReceivedAt: at,
		Measurements: []storage.Measurement{
			{Metric: "disk.free_bytes", Sensor: "disk", Labels: volume("/"), Value: gb(9), TS: at},
		},
	}); err != nil {
		t.Fatalf("SaveIngest: %v", err)
	}
	if err := db.SaveThreshold(context.Background(), storage.Threshold{
		Series:    storage.SeriesRef{Node: "laptop-a", Metric: "disk.free_bytes", Labels: volume("/")},
		Direction: storage.Below,
		Warning:   num(gb(10)),
		Critical:  num(gb(4)),
	}); err != nil {
		t.Fatalf("SaveThreshold: %v", err)
	}

	channel := &recorder{}
	pass(t, digesting(db, channel, at, first, second))

	summaries := channel.summaries()
	if len(summaries) != 1 {
		t.Fatalf("three warnings on two nodes sent %d messages", len(summaries))
	}
	var got []string
	for _, entry := range summaries[0] {
		got = append(got, entry.Node+" "+entry.Labels["mount"])
	}
	want := []string{"laptop-a /", "server-b /data", "server-b /srv"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("the digest reads %v, want %v", got, want)
		}
	}
}

// spec: evaluation.md#digest — a frozen subject in warning is left out: its data is stale,
// so it is neither a transition nor a current reading.
func TestAFrozenWarningIsLeftOutOfTheDigest(t *testing.T) {
	db := open(t)
	// The transition is older than the window the digest reports on, so all that is left
	// of this subject is a stale reading standing in warning.
	warned(t, db, yesterday.Add(time.Hour), "/data")
	mark(t, db, occurrence.Add(-time.Minute))

	channel := &recorder{}
	pass(t, digesting(db, channel, occurrence.Add(time.Hour), watching(t)))

	for _, summary := range channel.summaries() {
		for _, entry := range summary {
			if entry.Labels["mount"] == "/data" {
				t.Fatalf("a stale reading was digested as standing in warning: %+v", entry)
			}
		}
	}
}

// spec: evaluation.md#digest — a frozen subject whose move into warning was recorded while
// its values were fresh is listed as the transition it was: freezing withholds judgement,
// not a record already written.
func TestAFrozenSubjectsRecordedTransitionIsDigested(t *testing.T) {
	db := open(t)
	// The warning is recorded inside the window; two hours later its values are stale,
	// and the digest hour has passed.
	warned(t, db, occurrence.Add(-time.Hour), "/data")
	mark(t, db, yesterday)

	channel := &recorder{}
	pass(t, digesting(db, channel, occurrence.Add(time.Hour), watching(t)))

	summaries := channel.summaries()
	if len(summaries) != 1 || len(summaries[0]) != 1 {
		t.Fatalf("the digest went out as %v, want the one recorded transition", summaries)
	}
	if got := summaries[0][0]; got.To != evaluate.Warning || got.Labels["mount"] != "/data" {
		t.Fatalf("the entry is %+v, want the frozen subject's recorded move into warning", got)
	}
}

// spec: evaluation.md#digest — a refused digest leaves the window open, so the next tick
// sends the same one again.
func TestARefusedDigestIsSentAgain(t *testing.T) {
	db := open(t)
	mark(t, db, yesterday)
	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/data"), gb(9))

	down := &recorder{digestFails: true}
	pass(t, digesting(db, down, at, watching(t)))
	if got, _ := lastDigest(t, db); !got.Equal(yesterday) {
		t.Fatalf("a refused digest moved the window to %v", got)
	}

	channel := &recorder{}
	later := at.Add(time.Minute)
	collect(t, db, later, volume("/data"), gb(9))
	pass(t, digesting(db, channel, later, watching(t)))
	if got := channel.summaries(); len(got) != 1 || len(got[0]) != 1 {
		t.Fatalf("the retry sent %v", got)
	}
}

// spec: evaluation.md#digest — a restart between a warning transition and `digest.at` keeps
// the transition in the digest: it was recorded when it happened.
func TestARestartKeepsTheTransitionInTheDigest(t *testing.T) {
	db := open(t)
	warned(t, db, yesterday.Add(time.Hour), "/data")
	mark(t, db, yesterday)

	// Each call builds a fresh evaluator, as a restarted hub would: nothing about the
	// window is held in memory.
	channel := &recorder{}
	pass(t, digesting(db, channel, yesterday.Add(2*time.Hour), watching(t)))
	if got := channel.summaries(); len(got) != 0 {
		t.Fatalf("the digest went out before its hour: %v", got)
	}

	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/data"), gb(9))
	fresh := &recorder{}
	pass(t, digesting(db, fresh, at, watching(t)))

	summaries := fresh.summaries()
	if len(summaries) != 1 || len(summaries[0]) != 1 {
		t.Fatalf("the restarted hub digested %v", summaries)
	}
	entry := summaries[0][0]
	if entry.From != evaluate.OK || !entry.At.Equal(yesterday.Add(time.Hour)) {
		t.Fatalf("the entry is %+v, want the transition recorded before the restart", entry)
	}
}

// spec: evaluation.md#digest — a digest that already went out today is not sent twice,
// whatever restarts in between.
func TestADigestAlreadySentIsNotRepeated(t *testing.T) {
	db := open(t)
	mark(t, db, occurrence)
	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/data"), gb(9))

	channel := &recorder{}
	pass(t, digesting(db, channel, at, watching(t)))
	if got := channel.summaries(); len(got) != 0 {
		t.Fatalf("the digest went out twice in one day: %v", got)
	}
}

// spec: evaluation.md#digest — a hub down at `digest.at` sends it on the first tick after
// startup.
func TestADigestMissedWhileDownGoesOutAtStartup(t *testing.T) {
	db := open(t)
	mark(t, db, yesterday)
	late := occurrence.Add(5 * time.Hour)
	collect(t, db, late, volume("/data"), gb(9))

	channel := &recorder{}
	pass(t, digesting(db, channel, late, watching(t)))
	if got := channel.summaries(); len(got) != 1 {
		t.Fatalf("a hub started after its hour digested %v", got)
	}
	if got := channel.digestedAt; len(got) != 1 || !got[0].Equal(occurrence) {
		t.Fatalf("the digest is stamped %v, want the occurrence %v", got, occurrence)
	}
}

// spec: evaluation.md#digest — a hub down for two days sends one digest, not two: only the
// most recent occurrence counts.
func TestTwoMissedDaysSendOneDigest(t *testing.T) {
	db := open(t)
	mark(t, db, occurrence.AddDate(0, 0, -3))
	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/data"), gb(9))

	channel := &recorder{}
	pass(t, digesting(db, channel, at, watching(t)))
	if got := channel.summaries(); len(got) != 1 {
		t.Fatalf("three missed days digested %d times", len(got))
	}
	if got, _ := lastDigest(t, db); !got.Equal(at) {
		t.Fatalf("the window closed at %v, want the tick %v that read it", got, at)
	}
}

// spec: evaluation.md#digest — a subject that returned from warning to ok inside the window
// is listed as the recovery it was: nothing delivered it at once.
func TestARecoveryFromWarningIsCarriedByTheDigest(t *testing.T) {
	db := open(t)
	warned(t, db, yesterday.Add(time.Hour), "/data")
	mark(t, db, yesterday)

	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/data"), gb(40))
	channel := &recorder{}
	pass(t, digesting(db, channel, at, watching(t)))

	if got := channel.sent(); len(got) != 0 {
		t.Fatalf("a recovery from warning was delivered at once: %+v", got)
	}
	summaries := channel.summaries()
	if len(summaries) != 1 || len(summaries[0]) != 1 {
		t.Fatalf("the digest carried %v, want the recovery", summaries)
	}
	if entry := summaries[0][0]; entry.From != evaluate.Warning || entry.To != evaluate.OK {
		t.Fatalf("the entry reads %v → %v, want warning → ok", entry.From, entry.To)
	}
}

// spec: evaluation.md#digest — a subject that reached critical inside the window was
// reported instantly, so its earlier move into warning is not repeated by the digest.
func TestAWarningOvertakenByCriticalIsNotDigested(t *testing.T) {
	db := open(t)
	mark(t, db, yesterday)

	warning := occurrence.Add(time.Hour)
	collect(t, db, warning, volume("/data"), gb(9))
	pass(t, digesting(db, &recorder{}, warning, watching(t)))
	mark(t, db, yesterday)

	critical := warning.Add(time.Minute)
	collect(t, db, critical, volume("/data"), gb(3))
	channel := &recorder{}
	pass(t, digesting(db, channel, critical, watching(t)))

	if got := channel.summaries(); len(got) != 0 {
		t.Fatalf("the digest repeated a warning the critical had overtaken: %v", got)
	}
	if got := channel.sent(); len(got) != 1 || got[0].To != evaluate.Critical {
		t.Fatalf("the critical delivered %+v, want one instant message", got)
	}
}

// spec: evaluation.md#digest — a transition recorded after `digest.at` by the tick that
// sent the digest is not listed again the next day: the window closed where it was read.
func TestATransitionReportedOnceIsNotDigestedAgainTomorrow(t *testing.T) {
	db := open(t)
	warned(t, db, yesterday.Add(time.Hour), "/data")
	// The last digest went out after that warning, so the window below starts clean.
	mark(t, db, yesterday.Add(3*time.Hour))

	// The hub was down at 09:00 and starts an hour later, so this tick both records the
	// recovery and sends the day's digest carrying it.
	late := occurrence.Add(time.Hour)
	collect(t, db, late, volume("/data"), gb(40))
	first := &recorder{}
	pass(t, digesting(db, first, late, watching(t)))
	summaries := first.summaries()
	if len(summaries) != 1 || len(summaries[0]) != 1 || summaries[0][0].To != evaluate.OK {
		t.Fatalf("the first digest carried %v, want the recovery", summaries)
	}

	// A day passes with nothing happening to the volume at all.
	tomorrow := late.AddDate(0, 0, 1)
	collect(t, db, tomorrow, volume("/data"), gb(40))
	second := &recorder{}
	pass(t, digesting(db, second, tomorrow, watching(t)))

	if got := second.summaries(); len(got) != 0 {
		t.Fatalf("the recovery was digested a second time: %v", got)
	}
}

// spec: evaluation.md#digest — the hub's first start is written down on the first tick, so
// a restart before the first digest ever goes out does not move the window forward and
// drop what was recorded in between.
func TestTheFirstStartSurvivesARestart(t *testing.T) {
	db := open(t)

	// The first hub starts after its digest hour, so it never reaches one: nothing but
	// the first start itself can mark where its window began.
	started := occurrence.AddDate(0, 0, -1).Add(time.Hour)
	recorded := started.Add(time.Hour)
	collect(t, db, recorded, volume("/data"), gb(9))
	pass(t, evaluate.New(evaluate.Options{
		Store: db, Notifier: &recorder{}, Targets: []evaluate.Target{watching(t)},
		Digest: schedule, Started: started, Now: func() time.Time { return recorded },
	}))

	// It restarts the next day, so its own idea of "first start" moves; the database's
	// does not, and the window still reaches back over the transition.
	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/data"), gb(40))
	channel := &recorder{}
	pass(t, evaluate.New(evaluate.Options{
		Store: db, Notifier: channel, Targets: []evaluate.Target{watching(t)},
		Digest: schedule, Started: at, Now: func() time.Time { return at },
	}))

	summaries := channel.summaries()
	if len(summaries) != 1 || len(summaries[0]) != 1 || summaries[0][0].To != evaluate.OK {
		t.Fatalf("the restarted hub digested %v, want the recovery it recorded", summaries)
	}
}

// spec: evaluation.md#digest — a first pass that stops before its digest still leaves the
// window it recorded into: the start is written down before any subject is.
func TestAnInterruptedFirstPassStillOpensTheWindow(t *testing.T) {
	db := open(t)
	started := occurrence.AddDate(0, 0, -1).Add(time.Hour)
	recorded := started.Add(time.Hour)
	collect(t, db, recorded, volume("/"), gb(9))
	collect(t, db, recorded, volume("/data"), gb(9))

	// The hub is asked to stop the moment its first change is recorded, so the pass never
	// reaches its digest.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopping := &cancelling{Store: db, cancel: cancel}
	interrupted := evaluate.New(evaluate.Options{
		Store: stopping, Notifier: &recorder{}, Targets: []evaluate.Target{watching(t)},
		Digest: schedule, Started: started, Now: func() time.Time { return recorded },
	})
	if err := interrupted.Tick(ctx); err == nil {
		t.Fatal("the interrupted pass reported success")
	}

	// It restarts the next day, so its own idea of "first start" has moved.
	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/"), gb(40))
	channel := &recorder{}
	pass(t, evaluate.New(evaluate.Options{
		Store: db, Notifier: channel, Targets: []evaluate.Target{watching(t)},
		Digest: schedule, Started: at, Now: func() time.Time { return at },
	}))

	if got := channel.summaries(); len(got) != 1 {
		t.Fatalf("the restarted hub digested %v, want the window the interrupted pass opened", got)
	}
}

// spec: evaluation.md#digest — a hub where nothing is watched says so every day, because
// silence while all is well means nothing when nothing is being judged.
func TestAHubThatWatchesNothingSaysSoDaily(t *testing.T) {
	db := open(t)
	mark(t, db, yesterday)
	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/"), gb(40))
	unwatch(t, db, volume("/"))

	channel := &recorder{}
	pass(t, digesting(db, channel, at, watching(t)))

	summaries := channel.summaries()
	if len(summaries) != 1 || len(summaries[0]) != 0 {
		t.Fatalf("a hub judging nothing sent %v, want one empty digest", summaries)
	}
	if got := channel.unwatchedCounts(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("the digest counted %v unwatched series, want [1]", got)
	}
}

// spec: evaluation.md#digest — a digest that is sent names how many series have no
// threshold, so a volume nobody configured is visible.
func TestTheDigestNamesTheSeriesNobodyWatches(t *testing.T) {
	db := open(t)
	warned(t, db, yesterday.Add(time.Hour), "/")
	at := occurrence.Add(time.Hour)
	collect(t, db, at, volume("/"), gb(9))
	collect(t, db, at, volume("/data"), gb(40))
	unwatch(t, db, volume("/data"))

	channel := &recorder{}
	pass(t, digesting(db, channel, at, watching(t)))

	if got := channel.unwatchedCounts(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("the digest counted %v unwatched series, want [1]", got)
	}
	if summaries := channel.summaries(); len(summaries) != 1 || len(summaries[0]) != 1 {
		t.Fatalf("the digest listed %v, want the one warning", summaries)
	}
}
