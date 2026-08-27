package lsp

import (
	"os"
	"sort"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/lspdoctor"
)

// doctor.go implements lsp.doctor (#2164): gather every configured language
// server's effective spec (after config overlays — exactly what the manager
// would launch) and hand them to the app's LSP Doctor tool window, which runs
// the per-server check chain and diagnoses failures with concrete, verifiable
// fix suggestions.

// doctor runs the palette command.
func doctor(h host.API) tea.Cmd {
	servers := doctorServers()
	return h.Dispatch(ilsp.DoctorMsg{Servers: servers})
}

// doctorServers resolves the doctor's probe set: one entry per language that
// resolves to a launchable server spec. Delegating languages (go.mod → go,
// #1063) are skipped — their delegate carries the server.
func doctorServers() []lspdoctor.Server {
	root, _ := os.Getwd()
	var out []lspdoctor.Server
	for _, l := range lang.All() {
		if l.Server == nil || l.ServerLang() != l.ID {
			continue
		}
		spec, ok := resolveSpec(l.ID)
		if !ok {
			continue
		}
		out = append(out, lspdoctor.Server{
			Lang:    l.ID,
			Command: spec.Command,
			Args:    spec.Args,
			Env:     spec.Env,
			Install: spec.Install,
			Root:    root,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lang < out[j].Lang })
	return out
}
