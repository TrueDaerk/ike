package httpfile

import (
	"fmt"
	"strings"
)

// HostVar is the variable a relative request target is resolved against.
const HostVar = "host"

// IsRelativeTarget reports whether a request line's unresolved target is a
// path that needs a host in front (`GET /do/stuff`). A target opening with a
// placeholder or a protocol-relative `//host` is not relative.
func IsRelativeTarget(target string) bool {
	return strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//")
}

// Value resolves a single user-defined variable through the chain, exactly
// as a `{{name}}` placeholder would, nested placeholders included.
func (v *Vars) Value(name string) (string, bool) {
	if v == nil {
		return "", false
	}
	return v.value(name, nil)
}

// AbsoluteTarget returns resolved's target made absolute: a relative target
// of the unresolved request r is prefixed with the `host` variable, or the
// request's Host header when no such variable resolves. A host without a
// scheme gets http. Any other target is returned unchanged.
func AbsoluteTarget(r, resolved *Request, vars *Vars) (string, error) {
	if !IsRelativeTarget(r.Target) {
		return resolved.Target, nil
	}
	host, ok := vars.Value(HostVar)
	if !ok || strings.TrimSpace(host) == "" {
		host, ok = "", false
		for _, h := range resolved.Headers {
			if strings.EqualFold(h.Name, "Host") && strings.TrimSpace(h.Value) != "" {
				host, ok = h.Value, true
				break
			}
		}
	}
	if !ok {
		return "", fmt.Errorf("request %s: relative target %q needs a @%s variable or Host header", r.Key(), resolved.Target, HostVar)
	}
	host = strings.TrimSpace(host)
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return strings.TrimRight(host, "/") + resolved.Target, nil
}
