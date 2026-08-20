package logline

import "strings"

// origin.go holds the origin separator of a merged rotated log set (#1996):
// the merged timeline is one buffer assembled from several files, so each
// region opens with a line naming the file it came from. The line is produced
// by internal/logset and recognized again here, so the log span stream can
// style it — the two sides share one format instead of guessing at each
// other's.
//
// The fence runes are deliberately outside anything a logging framework
// emits, and the whole line is a *span*, not buffer structure: toggling log
// rendering off shows it raw like every other layer.

// originFence brackets an origin separator on both sides.
const originFence = "────"

// OriginLine renders the origin separator for the member file name — the line
// internal/logset writes above each region of a merged timeline.
func OriginLine(name string) string {
	return originFence + " " + name + " " + originFence
}

// OriginName reports the file name an origin separator names, and whether
// line is one at all. A line merely *containing* the fence is not one: the
// separator is the whole line, fence to fence.
func OriginName(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, originFence+" ")
	if !ok {
		return "", false
	}
	name, ok := strings.CutSuffix(rest, " "+originFence)
	if !ok || name == "" || strings.Contains(name, originFence) {
		return "", false
	}
	return name, true
}
