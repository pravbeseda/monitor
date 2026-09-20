// Package evaluate turns stored measurements into meaning: every watched subject gets a
// level, a change of level becomes an event, and events become notifications
// (docs/specs/evaluation.md).
package evaluate

// SilenceMetric is the metric of the subject that carries a node's own silence. Nothing
// is stored for it and nothing can be: it has no threshold, only the window its class
// resolves to, and its input is hub receipt time, which is always fresh (ADR 0032).
const SilenceMetric = "silence"

// StaleFactor turns a sensor's interval into the age at which its values stop being
// evidence: three collections missed is no longer a hiccup. It is exported because a
// history chart breaks its line at the same age (docs/specs/history.md#gaps), and two
// copies of the number would let the two drift apart.
const StaleFactor = 3
