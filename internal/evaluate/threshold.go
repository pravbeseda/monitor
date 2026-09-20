package evaluate

import (
	"math"

	"github.com/pravbeseda/monitor/internal/storage"
)

// A subject is entered into a level by the strict comparison its direction names, and
// leaves it when the value clears the threshold by 20% of its magnitude in the other
// direction (ADR 0013, ADR 0033). A level with no value is neither entered nor held.
//
// The exit is compared as a distance — how far the value has moved past the threshold,
// against the margin — rather than as the value against threshold ± margin. Both say the
// same thing, but the difference cannot overflow the way `1.2 × 1e308` does, and it keeps
// its meaning for a threshold small enough that an absolute slack would swallow its whole
// margin. tolerance is relative for the same reason: it absorbs the float error of the
// products and nothing else.
func margin(threshold float64) float64 { return math.Abs(threshold) / 5 }

func tolerance(threshold float64) float64 { return 1e-9 * math.Abs(threshold) }

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
func entered(threshold float64, direction storage.Direction, value float64) bool {
	if direction == storage.Above {
		return value > threshold
	}
	return value < threshold
}

// cleared is the negation of entered with the margin: how far the value has moved past
// the threshold, against how far it has to move.
func cleared(threshold float64, direction storage.Direction, value float64) bool {
	moved := value - threshold
	if direction == storage.Above {
		moved = threshold - value
	}
	return moved >= margin(threshold)-tolerance(threshold)
}

// levelOf is the level a subject reaches from previous at this value. Levels are tried
// most severe first: a level is entered when its comparison holds, and held when the
// subject was already at least that severe and has not cleared it.
func levelOf(th storage.Threshold, previous Level, value float64) Level {
	for _, level := range []Level{Critical, Warning} {
		// A level with no value is never entered and never held: nothing can hold a
		// subject at a level nobody configured.
		at := thresholdOf(th, level)
		if at == nil {
			continue
		}
		if entered(*at, th.Direction, value) {
			return level
		}
		if previous >= level && !cleared(*at, th.Direction, value) {
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
