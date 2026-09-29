package jqplay

import (
	"errors"
	"strings"
)

// chain.go turns a result into the next input (#2795): the playground's
// drill-down, where `.data.items` is run once and the program starts over on
// that subtree instead of growing a longer and longer prefix.

// ErrChainXMQ is Chain's refusal for an xmq result: the xmq engine is the
// external binary, whose outputs are text in whatever notation the command
// wrote — there are no decoded values to hand to the next run.
var ErrChainXMQ = errors.New("xmq results cannot be chained — their outputs are text, not values")

// ErrChainEmpty is Chain's refusal for a result that produced no value.
var ErrChainEmpty = errors.New("the result has no values to chain")

// Chain returns the result's values as a new Input in the result's own
// dialect — the snapshot the next level of the chain queries — together with
// that input's text in the dialect's default spelling (pretty, one value per
// line / document), which is what a saved filter's sample captures. The
// values are the run's own, never re-parsed from the buffer, so a raw (-r) or
// compact (-c) rendering does not change what is chained: the strings of a
// -r result chain as strings.
//
// A result cut at MaxOutputs or MaxResultBytes chains what was collected; the
// input's origin says so.
func (r Result) Chain() (*Input, string, error) {
	if r.dialect == DialectXMQ {
		return nil, "", ErrChainXMQ
	}
	if len(r.values) == 0 {
		return nil, "", ErrChainEmpty
	}
	text := r.Text()
	if r.opts.Raw || r.opts.Compact {
		parts := make([]string, len(r.values))
		for i, v := range r.values {
			parts[i] = r.dialect.encode(v)
		}
		text = strings.Join(parts, r.dialect.separator())
	}
	origin := "chained"
	if r.Truncated {
		origin = "chained from a capped result"
	}
	in := &Input{
		values:  append([]any(nil), r.values...),
		size:    len(text),
		dialect: r.dialect,
		origin:  origin,
	}
	return in, text, nil
}
