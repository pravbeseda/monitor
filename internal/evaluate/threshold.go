package evaluate

import (
	"math"

	"github.com/pravbeseda/monitor/internal/storage"
)

// A subject is entered into a level by the strict comparison its direction names, and
// leaves it when the value clears the threshold by 20% of its magnitude in the other
// direction (ADR 0013, ADR 0033). A level with no value is neither entered nor held.
//
// The margin is applied as 5×value against 5×threshold ± |threshold| rather than as
// value against 1.2×threshold, because 1.2 has no exact binary form and a value sitting
// exactly on its clearing value must count as cleared. tolerance absorbs what is left of
// the float error, scaled to the threshold so that it cannot bridge the gap between two
// values a sensor can tell apart.
const marginDenominator = 5

func tolerance(threshold float64) float64 {
	return 1e-9 * math.Max(1, math.Abs(threshold))
}

// Readable reports whether a stored threshold is one this build can judge by: a direction
// it knows, and values that are finite. Anything else is skipped rather than guessed at —
// an unknown direction read as its opposite would alert upside down, and a NaN is never
// entered and never left, so it would latch a subject at its level for good
// (docs/specs/evaluation.md#configuration-changes).
func Readable(th storage.Threshold) bool {
	if th.Direction != storage.Below && th.Direction != storage.Above {
		return false
	}
	for _, value := range []*float64{th.Warning, th.Critical} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
			return false
		}
	}
	return true
}

// entered reports whether a value falls into a level at all.
func entered(threshold *float64, direction storage.Direction, value float64) bool {
	if threshold == nil {
		return false
	}
	if direction == storage.Above {
		return value > *threshold
	}
	return value < *threshold
}

// cleared is the negation of entered with the margin. A level with no value counts as
// cleared, so that nothing can hold a subject at a level nobody configured.
func cleared(threshold *float64, direction storage.Direction, value float64) bool {
	if threshold == nil {
		return true
	}
	t := *threshold
	margin := math.Abs(t)
	if direction == storage.Above {
		return marginDenominator*value <= marginDenominator*t-margin+tolerance(t)
	}
	return marginDenominator*value+tolerance(t) >= marginDenominator*t+margin
}

// levelOf is the level a subject reaches from previous at this value. Levels are tried
// most severe first: a level is entered when its comparison holds, and held when the
// subject was already at least that severe and has not cleared it.
func levelOf(th storage.Threshold, previous Level, value float64) Level {
	for _, level := range []Level{Critical, Warning} {
		threshold := thresholdOf(th, level)
		if threshold == nil {
			continue
		}
		if entered(threshold, th.Direction, value) {
			return level
		}
		if previous >= level && !cleared(threshold, th.Direction, value) {
			return level
		}
	}
	return OK
}

func thresholdOf(th storage.Threshold, level Level) *float64 {
	if level == Critical {
		return th.Critical
	}
	return th.Warning
}
