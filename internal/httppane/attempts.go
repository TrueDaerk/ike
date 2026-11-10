package httppane

// attempts.go renders the earlier attempts of a `# @retry` series (#3116).
// The pane shows the final attempt's response; the status line carries the
// "attempt 3/4" indicator, and the block under it lists every attempt before
// the shown one — its outcome (status or error) and how long it took — so
// "it worked on the third try, after two 503s" is read off the pane instead
// of guessed at. It folds like the redirect chain (redirects.go): collapsed
// up front once it is long, toggled with enter, copied whole with y.

import (
	"fmt"

	"ike/internal/highlight"
	"ike/internal/httpclient"
)

// attemptGlyph leads every row of the block — the "and then again" arrow.
const attemptGlyph = "↻"

// pluralAttempts spells the block's header.
func pluralAttempts(n int) string {
	if n == 1 {
		return "1 earlier attempt"
	}
	return fmt.Sprintf("%d earlier attempts", n)
}

// attemptRows composes the block: a header row the fold hangs on, then one
// row per earlier attempt. nil when there were none.
func attemptRows(attempts []httpclient.Attempt) []row {
	if len(attempts) == 0 {
		return nil
	}
	rows := []row{{kind: kindAttempts, text: pluralAttempts(len(attempts))}}
	for i, a := range attempts {
		rows = append(rows, row{kind: kindAttempts,
			text: fmt.Sprintf("%s #%d  %s  (%s)", attemptGlyph, i+1, a.Outcome(), roundDuration(a.Duration))})
	}
	return rows
}

// setAttempts records the composed block's fold, collapsed up front once the
// series is longer than the chain's threshold. Like the chain's it is kept
// apart from the body's folds and re-added by setFolds.
func (m *Model) setAttempts(header, end, n int) {
	m.attemptFold = highlight.Fold{HeaderLine: header, EndLine: end}
	m.attemptOK = end > header
	if !m.attemptOK {
		return
	}
	m.folds = append(m.folds, m.attemptFold)
	if n > chainFoldThreshold {
		if m.folded == nil {
			m.folded = map[int]int{}
		}
		m.folded[header] = end
	}
}

// attemptsTargeted reports whether the block is what enter and y act on —
// the fold rule of fold.go, as for the redirect chain.
func (m *Model) attemptsTargeted() bool {
	return m.attemptOK && m.targetFold() == m.attemptFold.HeaderLine
}

// headFoldTargeted returns the header row of the block above the body —
// redirect chain or earlier attempts — that enter folds, if either is
// targeted.
func (m *Model) headFoldTargeted() (int, bool) {
	if m.chainTargeted() {
		return m.chainRow(), true
	}
	if m.attemptsTargeted() {
		return m.attemptFold.HeaderLine, true
	}
	return 0, false
}

// AttemptsText is the whole block as plain text — what "y" copies while it is
// the targeted fold, collapsed or not.
func (m *Model) AttemptsText() string {
	if !m.attemptOK {
		return ""
	}
	return m.foldRangeText(m.attemptFold.HeaderLine, m.attemptFold.EndLine)
}

// AttemptRows reports the row range of the block and whether there is one
// (tests).
func (m *Model) AttemptRows() (start, end int, ok bool) {
	return m.attemptFold.HeaderLine, m.attemptFold.EndLine, m.attemptOK
}

// SetPendingAttempt records which attempt of a `# @retry` series is in flight
// (#3116); the host calls it with SetPending when the countdown restarts for
// the next attempt. n <= 1 clears it.
func (m *Model) SetPendingAttempt(n, max int) {
	if n <= 1 || max <= 1 {
		m.pendingAttempt = ""
		return
	}
	m.pendingAttempt = fmt.Sprintf("attempt %d/%d", n, max)
}

// PendingAttempt reports the in-flight attempt indicator (tests).
func (m *Model) PendingAttempt() string { return m.pendingAttempt }

// pendingAttemptPrefix is the in-flight header's attempt part, "attempt 2/4 ·
// ", or "" for a first attempt.
func (m *Model) pendingAttemptPrefix() string {
	if m.pendingAttempt == "" {
		return ""
	}
	return m.pendingAttempt + " · "
}
