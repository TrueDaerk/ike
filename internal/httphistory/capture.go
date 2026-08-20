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
	type stamped struct {
		value string
		at    int64
	}
	newest := map[string]stamped{}
	for _, key := range keys {
		for _, e := range s.List(source, key) {
			at := e.Time.UnixNano()
			for name, value := range e.Captured {
				if prev, ok := newest[name]; ok && prev.at >= at {
					continue
				}
				newest[name] = stamped{value: value, at: at}
			}
		}
	}
	if len(newest) == 0 {
		return nil
	}
	out := make(map[string]string, len(newest))
	for name, v := range newest {
		out[name] = v.value
	}
	return out
}
