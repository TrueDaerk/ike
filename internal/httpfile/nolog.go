package httpfile

// nolog.go parses the `# @no-log` directive (#3115): the per-request opt-out
// of the on-disk response history, for an exchange whose answer carries data
// that should not be persisted (tokens, personal data, dumps). Like
// `# @no-curlrc` (#3114) it is a value-less comment line the parser only
// *recognises*; keeping the exchange off disk happens in the app, where the
// finished exchange would otherwise be written.

import (
	"fmt"
	"regexp"
)

// NoLogDirective is the directive's name, without the `@`.
const NoLogDirective = "no-log"

// noLogRE matches the directive: a comment line (`#`, `##` or `//`) whose
// text is `@no-log`, optionally followed by a value — captured only so it can
// be rejected on its own line. Three hashes are not accepted, for the reason
// captureRE gives: `###` opens a new request block.
var noLogRE = regexp.MustCompile(`^[ \t]*(?:##?|//)[ \t]*@no-log(?:[ \t]+(\S.*?))?[ \t]*$`)

// NoLogDirectiveAt recognises a `# @no-log` line. ok is false when the line
// is not the directive; a directive followed by a value answers ok with err
// set — it takes none, and silently ignoring `# @no-log yes` would hide a
// misunderstanding. Exposed like ConfigSkipDirective: the highlighter reads
// exactly what the parser reads.
func NoLogDirectiveAt(line string) (err error, ok bool) {
	m := noLogRE.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}
	if m[1] != "" {
		return fmt.Errorf("@%s takes no value, got %q", NoLogDirective, m[1]), true
	}
	return nil, true
}
