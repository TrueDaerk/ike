package format

import "errors"

// Notice is a provider outcome that leaves the buffer unchanged without being
// a failure (e.g. "body has no recognised Content-Type"); callers show it as
// an info notice instead of an error.
type Notice string

func (n Notice) Error() string { return string(n) }

// AsNotice reports whether err is (or wraps) a Notice and returns its text.
func AsNotice(err error) (string, bool) {
	var n Notice
	if errors.As(err, &n) {
		return string(n), true
	}
	return "", false
}
