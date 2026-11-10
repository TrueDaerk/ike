package httppane

import (
	"fmt"

	"ike/internal/httpclient"
)

// TruncatedMarker is what the status row of a cut response says (#3117): the
// body ended at its cap — a `# @max-body` directive or the built-in limit —
// not where the server ended it. The warning row below the headers names the
// limit; the marker is where the eye rests first.
const TruncatedMarker = "truncated"

// truncatedNote is the line the assertion block carries for a truncated body
// (#3117): its verdicts were reached on the bytes read, and a failed `body
// contains` may simply be looking past the cut.
func truncatedNote(resp *httpclient.Response) string {
	return fmt.Sprintf("  (body truncated at %d bytes — evaluated on what was read)", resp.BodyBytes())
}
