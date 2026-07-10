package sdk

// host.go is the typed face of the host imports: what a plugin calls to
// affect the running editor.

import "encoding/json"

// Notify raises a toast in the editor.
func Notify(sev Severity, text string) {
	data, err := json.Marshal(struct {
		Severity int    `json:"severity"`
		Text     string `json:"text"`
	}{int(sev), text})
	if err != nil {
		return
	}
	ptr, length := regionOf(data)
	hostNotify(ptr, length)
}

// SetStatus replaces the plugin's persistent status-line segment. Use Notify
// for event-like messages; SetStatus for ongoing state.
func SetStatus(text string) {
	ptr, length := regionOf([]byte(text))
	hostSetStatus(ptr, length)
}

// OpenFile asks the editor to open path.
func OpenFile(path string) {
	if path == "" {
		return
	}
	ptr, length := regionOf([]byte(path))
	hostOpenFile(ptr, length)
}

// Dispatch sends a typed message envelope to the host. Only types the host
// knows are acted on ("open_file" today); unknown types surface as a warning
// toast rather than being guessed.
func Dispatch(msgType string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}{msgType, body})
	if err != nil {
		return err
	}
	ptr, length := regionOf(data)
	hostDispatch(ptr, length)
	return nil
}

// ConfigGet reads one dotted configuration key (e.g. "editor.tab_width");
// ok is false when the key is absent.
func ConfigGet(key string) (value string, ok bool) {
	ptr, length := regionOf([]byte(key))
	packed := hostConfigGet(ptr, length)
	if packed == 0 {
		return "", false
	}
	vptr, vlen := uint32(packed>>32), uint32(packed)
	return string(readRegion(vptr, vlen)), true
}
