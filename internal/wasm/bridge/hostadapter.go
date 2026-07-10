package bridge

import (
	"encoding/json"
	"sync"

	"ike/internal/host"
	"ike/internal/wasm/abi"
)

// hostadapter.go adapts the real host.API onto the ABI's narrow Host
// interface. The adapter binds late: the wasm runtime (and its host module)
// must exist before the app model does, so guest calls arriving before
// SetAPI are dropped silently — nothing meaningful can happen that early.

// HostAdapter implements abi.Host over a late-bound host.API.
type HostAdapter struct {
	mu  sync.Mutex
	api host.API
}

// NewHostAdapter returns an unbound adapter; bind it with SetAPI once the
// app's host exists.
func NewHostAdapter() *HostAdapter { return &HostAdapter{} }

// SetAPI binds the live host.
func (a *HostAdapter) SetAPI(api host.API) {
	a.mu.Lock()
	a.api = api
	a.mu.Unlock()
}

func (a *HostAdapter) get() host.API {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.api
}

// OpenFile implements abi.Host: the request re-enters the Update loop via
// Send (guest calls run on cmd goroutines, never inside Update).
func (a *HostAdapter) OpenFile(path string) {
	if api := a.get(); api != nil {
		api.Send(host.OpenFileRequest{Path: path})
	}
}

// Dispatch implements abi.Host: well-known envelope types map onto concrete
// messages; unknown types are rejected with a warning, never guessed.
func (a *HostAdapter) Dispatch(env abi.DispatchEnvelope) {
	api := a.get()
	if api == nil {
		return
	}
	switch env.Type {
	case "open_file":
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(env.Payload, &p); err == nil && p.Path != "" {
			api.Send(host.OpenFileRequest{Path: p.Path})
		}
	default:
		api.Notify(host.Warn, "wasm plugin dispatched unknown message type "+env.Type)
	}
}

// Notify implements abi.Host with the severity mapping 0/1/2 → info/warn/error.
func (a *HostAdapter) Notify(n abi.Notification) {
	api := a.get()
	if api == nil {
		return
	}
	sev := host.Info
	switch n.Severity {
	case 1:
		sev = host.Warn
	case 2:
		sev = host.Error
	}
	api.Notify(sev, n.Text)
}

// SetStatus implements abi.Host.
func (a *HostAdapter) SetStatus(text string) {
	if api := a.get(); api != nil {
		api.SetStatus(text)
	}
}

// ConfigGet implements abi.Host.
func (a *HostAdapter) ConfigGet(key string) (string, bool) {
	api := a.get()
	if api == nil {
		return "", false
	}
	cfg := api.Config()
	if cfg == nil {
		return "", false
	}
	return cfg.Get(key)
}
