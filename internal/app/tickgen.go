package app

import "sync/atomic"

// tickgen.go carries the model generation that stamps every demand-armed
// debounce tick (#2194). Each *TickArmed guard lives on the Model *value*,
// but a project switch rebuilds the model (buildModel) while a tick minted by
// the departed model can still be sleeping — up to its whole interval. The
// fresh model's zeroed flag then lets a second chain arm on the same clock:
// for the follow poll both chains self-sustain while a view keeps following,
// one extra chain per park/resume race. The generation makes the guard
// structural instead of a per-site invariant: a tick names the model that
// minted it, and a model handles only the ticks it owns.
//
// The pattern is the one preview.RenderTickMsg, palette.LiveTickMsg,
// terminal.AutoScrollMsg, playDebounceMsg and the explorer's pollMsg (#2163)
// already use.

// modelSeq mints process-wide model generations. Process-wide (not per-model
// counting) so a stale tick can never collide with a fresh model's stamp,
// whatever the switch/resume history.
var modelSeq atomic.Int64

// nextModelGen returns the generation for a model being built.
func nextModelGen() int64 { return modelSeq.Add(1) }

// ownsTick reports whether a generation-stamped tick belongs to this model.
// A tick from a departed model is dropped without a re-arm, which retires its
// chain; the live model's own flag is untouched, so its chain keeps running.
func (m Model) ownsTick(gen int64) bool { return gen == m.modelGen }
