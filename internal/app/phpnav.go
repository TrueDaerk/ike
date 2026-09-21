package app

// phpnav.go is the app's wiring of the trait navigation fallback (Epic 0520,
// #2670): the index's host-facing view (phpindex.HostView) registered on the
// host, so the LSP bridge can fall back to the declaration index for
// go-to-definition, peek and hover inside a trait body — after the server
// answered empty, never in front of it.
//
// Everything PHP-specific lives in the view; the app contributes only the
// telemetry recorder, the way it does for the trait completion source
// (#2668).

import (
	"strconv"

	"ike/internal/host"
	"ike/internal/phpindex"
	"ike/internal/telemetry"
)

// traitNavView builds the host view over the index and hangs the telemetry
// recorder on it.
func traitNavView(idx *phpindex.Index, rec *telemetry.Recorder) *phpindex.HostView {
	v := phpindex.NewHostView(idx)
	v.SetTelemetry(traitNavRecorder(rec))
	v.SetReferencesTelemetry(traitRefsRecorder(rec))
	return v
}

// traitRefsRecorder is the callback for a find-usages answer the index
// complemented (#2671): one php.trait.references event carrying the server's
// location count and the rows the index added.
func traitRefsRecorder(rec *telemetry.Recorder) func(server, index int) {
	if rec == nil {
		return nil
	}
	return func(server, index int) {
		rec.Op(telemetry.OpPHPTraitReferences, telemetry.OpPhaseOK, map[string]string{
			"server": strconv.Itoa(server),
			"index":  strconv.Itoa(index),
		})
	}
}

// traitNavRecorder is the callback the host view reports its non-empty
// answers through: one op event per answer, naming the feature and how many
// declarations the index resolved. It runs on the bridge's request
// goroutine, so it touches nothing but the recorder, which is safe there.
func traitNavRecorder(rec *telemetry.Recorder) func(host.TraitLookup, int) {
	if rec == nil {
		return nil
	}
	return func(op host.TraitLookup, n int) {
		id := telemetry.OpPHPTraitDefinition
		if op == host.TraitHover {
			id = telemetry.OpPHPTraitHover
		}
		rec.Op(id, telemetry.OpPhaseOK, map[string]string{"count": strconv.Itoa(n)})
	}
}
