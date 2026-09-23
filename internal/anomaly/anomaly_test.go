package anomaly_test

import (
	"testing"
	"time"

	"github.com/pravbeseda/monitor/internal/anomaly"
	"github.com/pravbeseda/monitor/internal/storage"
)

var start = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)

// week spreads values over the seven days of a norm period, so the rows about the score
// never trip over the rule about how much history a norm needs.
func week(values ...float64) []storage.Point {
	step := 7 * 24 * time.Hour / time.Duration(len(values))
	out := make([]storage.Point, len(values))
	for i, v := range values {
		out[i] = storage.Point{TS: start.Add(time.Duration(i) * step), Value: v}
	}
	return out
}

func repeat(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func upTo(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(i + 1)
	}
	return out
}

func joined(parts ...[]float64) []float64 {
	var out []float64
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

type scoreCase struct {
	name   string
	values []float64
	newest float64
	// score is nil when the value has no score: a side of no width
	score   *float64
	unusual bool
}

func num(v float64) *float64 { return &v }

// spec: anomaly.md#score
func TestAValueIsScoredInWidthsOfTheSideItIsOn(t *testing.T) {
	idle := joined(repeat(0.1, 95), repeat(2.0, 5))
	quiet := joined(repeat(0, 2014), []float64{0.5, 0.6})
	for _, tc := range []scoreCase{
		{"the norm itself", upTo(100), 50, num(0), false},
		{"the edge of the band", upTo(100), 99, num(1), false},
		{"short of twice the edge", upTo(100), 147, num(1.98), false},
		{"twice the edge is inclusive", upTo(100), 148, num(2), true},
		{"the lower edge", upTo(100), 1, num(-1), false},
		{"twice the lower edge", upTo(100), -48, num(-2), true},
		{"far above", upTo(100), 1000, num(19.39), true},

		{"a nightly job is part of the norm", idle, 2.0, num(1), false},
		{"twice the nightly job", idle, 3.9, num(2), true},
		{"idle", idle, 0.1, num(0), false},
		{"below an idle it never left", idle, 0.09, num(-10), true},

		{"a hair of load on a quiet machine", quiet, 0.01, num(0.02), false},
		{"the job it ran twice", quiet, 0.5, num(0.83), false},
		{"past the job it ran twice", quiet, 1.3, num(2.17), true},

		{"compared as reported", joined(repeat(1.99, 99), []float64{3.98}), 5.97, num(2), true},

		{"zeros all week", repeat(0, 100), 0, num(0), false},
		{"off zeros all week", repeat(0, 100), 1, nil, true},

		{"a failure fixed today", repeat(1, 100), 0, num(-100), true},
		{"a step the week already took", joined(repeat(1, 99), []float64{0}), 0, num(-1), false},
		{"a step the other side took does not widen this one", joined(repeat(1, 99), []float64{0}), 2, num(100), true},

		{"a rounding step on a static volume", repeat(62.13, 100), 62.12, num(-0.02), false},
		{"a static volume written to", repeat(62.13, 100), 60.8, num(-2.14), true},
		{"a static volume freed", repeat(62.13, 100), 65, num(4.62), true},

		{"within the floor of a negative norm", repeat(-5, 100), -5.04, num(-0.8), false},
		{"past the floor of a negative norm", repeat(-5, 100), -5.1, num(-2), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			norm, ok := anomaly.NormOf(week(tc.values...))
			if !ok {
				t.Fatal("no norm")
			}
			got := norm.Judge(tc.newest)
			switch {
			case tc.score == nil && got.Score != nil:
				t.Fatalf("score = %v, want none", *got.Score)
			case tc.score != nil && got.Score == nil:
				t.Fatalf("no score, want %v", *tc.score)
			case tc.score != nil && *got.Score != *tc.score:
				t.Fatalf("score = %v, want %v", *got.Score, *tc.score)
			}
			if got.Unusual() != tc.unusual {
				t.Fatalf("unusual = %v, want %v", got.Unusual(), tc.unusual)
			}
		})
	}
}

// spec: anomaly.md#model — the norm is a value the series reported: the median by nearest
// rank, the lower of the two middles for an even count.
func TestTheNormIsAValueTheSeriesReported(t *testing.T) {
	norm, _ := anomaly.NormOf(week(upTo(100)...))
	if got := norm.Judge(50).Norm; got != 50 {
		t.Fatalf("norm = %v, want 50", got)
	}
}

// spec: anomaly.md#model — −0 is reported as 0.
func TestANegligibleScoreIsNotNegativeZero(t *testing.T) {
	norm, _ := anomaly.NormOf(week(upTo(100)...))
	got := norm.Judge(49.9995).Score
	if got == nil || *got != 0 || 1 / *got < 0 {
		t.Fatalf("score = %v, want +0", got)
	}
}

// spec: anomaly.md#norm-period
func TestANormNeedsADayOfHistory(t *testing.T) {
	spaced := func(n int, span time.Duration) []storage.Point {
		out := make([]storage.Point, n)
		for i := range out {
			out[i] = storage.Point{TS: start.Add(span * time.Duration(i) / time.Duration(n-1)), Value: 1}
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		points []storage.Point
		ok     bool
	}{
		{"23 points over three days", spaced(23, 72*time.Hour), false},
		{"40 points inside 20 hours", spaced(40, 20*time.Hour), false},
		{"24 points exactly a day apart end to end", spaced(24, 24*time.Hour), true},
		{"nothing at all", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := anomaly.NormOf(tc.points); ok != tc.ok {
				t.Fatalf("norm = %v, want %v", ok, tc.ok)
			}
		})
	}
}

// spec: anomaly.md#norm-period — a laptop that reported 9 hourly points on each of three
// days of the period.
func TestALaptopAwakeThreeDaysHasANorm(t *testing.T) {
	var points []storage.Point
	for day := range 3 {
		for hour := range 9 {
			at := start.Add(time.Duration(day)*24*time.Hour + time.Duration(hour)*time.Hour)
			points = append(points, storage.Point{TS: at, Value: 1})
		}
	}
	if _, ok := anomaly.NormOf(points); !ok {
		t.Fatal("no norm")
	}
}

// spec: anomaly.md#norm-period
func TestThePeriodIsTheWeekBeforeYesterdayCountedFromTheHour(t *testing.T) {
	for _, tc := range []struct {
		now      time.Time
		from, to time.Time
	}{
		{
			time.Date(2026, 9, 23, 10, 20, 0, 0, time.UTC),
			time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
		},
		{
			time.Date(2026, 9, 23, 10, 59, 59, 0, time.UTC),
			time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
		},
		{
			time.Date(2026, 9, 23, 11, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 22, 11, 0, 0, 0, time.UTC),
		},
		{
			// The hour is the UTC hour, whatever zone the clock is read in.
			time.Date(2026, 9, 23, 16, 20, 0, 0, time.FixedZone("IST", 5*3600+1800)),
			time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
		},
	} {
		from, to := anomaly.Period(tc.now)
		if !from.Equal(tc.from) || !to.Equal(tc.to) {
			t.Fatalf("period at %v = %v – %v, want %v – %v", tc.now, from, to, tc.from, tc.to)
		}
	}
}

// spec: anomaly.md#rank
func TestTheMostUnusualRanksFirst(t *testing.T) {
	scored := func(v float64) *anomaly.Anomaly { return &anomaly.Anomaly{Score: num(v)} }
	unscored := func() *anomaly.Anomaly { return &anomaly.Anomaly{} }
	for _, tc := range []struct {
		name  string
		given []*anomaly.Anomaly
		ranks []int
	}{
		{"by magnitude", []*anomaly.Anomaly{scored(2.5), scored(-9), scored(4)}, []int{3, 1, 2}},
		{"no score first", []*anomaly.Anomaly{scored(40), unscored()}, []int{2, 1}},
		{"a tie keeps the given order", []*anomaly.Anomaly{scored(3), scored(-3)}, []int{1, 2}},
		{"two without a score keep the given order", []*anomaly.Anomaly{unscored(), unscored()}, []int{1, 2}},
		{"nothing unusual", []*anomaly.Anomaly{scored(1.5), scored(-0.3)}, []int{0, 0}},
		{"no gap and no repeat", []*anomaly.Anomaly{scored(2), scored(0.1), scored(5), nil, scored(3)}, []int{3, 0, 1, 0, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			anomaly.Rank(tc.given)
			for i, a := range tc.given {
				got := 0
				if a != nil {
					got = a.Rank
				}
				if got != tc.ranks[i] {
					t.Fatalf("rank of %d = %d, want %d", i, got, tc.ranks[i])
				}
			}
		})
	}
}
