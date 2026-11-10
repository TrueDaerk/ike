package httphistory

// capture.go answers "what did this .http file capture so far?" (#1993). The
// captured values of a response are stored with that response (Entry.Captured),
// so the lifetime question needs no extra state anywhere: a value is available
// exactly as long as the response it was taken from is — across a restart, and
// no longer than the five entries a request keeps.

// Captured collects the capture variables of one .http file: the stored
// responses of every request key handed in, newest value per name. "Newest"
// is the entry's own timestamp, not the order the keys arrive in, so a name
// captured by two different requests reads the value of whichever ran last —
// the same rule a re-run of one request follows.
//
// Entries that captured nothing (every response before #1993, and every
// re-send, which repeats a snapshot rather than a parsed request) contribute
// nothing and never shadow a value.
func (s *Store) Captured(source string, keys []string) map[string]string {
	return CapturedIn(keys, func(key string) []Entry { return s.List(source, key) })
}

// CapturedIn is Captured over any history reader (#3115): list returns the
// newest-first entries of one request key — the Store's, or the Store's
// merged with a Session's, so a value captured by a `# @no-log` exchange is
// available for the rest of the session.
func CapturedIn(keys []string, list func(key string) []Entry) map[string]string {
	from := CapturedFrom(keys, list)
	if len(from) == 0 {
		return nil
	}
	out := make(map[string]string, len(from))
	for name, c := range from {
		out[name] = c.Value
	}
	return out
}

// CapturedValue is one captured variable together with the stored response
// that produced it (#3122): the request key the entry is stored under and the
// entry itself, so a caller can name the request, age the value and show the
// very response it came from.
type CapturedValue struct {
	Value string
	Key   string
	Entry Entry
}

// CapturedFrom is CapturedIn keeping the provenance of each value: the same
// newest-entry-wins rule, but every name maps to the entry (and its request
// key) that captured the winning value.
func CapturedFrom(keys []string, list func(key string) []Entry) map[string]CapturedValue {
	newest := map[string]CapturedValue{}
	for _, key := range keys {
		for _, e := range list(key) {
			for name, value := range e.Captured {
				if prev, ok := newest[name]; ok && !e.Time.After(prev.Entry.Time) {
					continue
				}
				newest[name] = CapturedValue{Value: value, Key: key, Entry: e}
			}
		}
	}
	if len(newest) == 0 {
		return nil
	}
	return newest
}
