package app

import (
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"ike/internal/diag"
	"ike/internal/host"
	"ike/internal/plugin"
)

// memory_cmd.go surfaces the runtime memory diagnostics (#1537) as palette
// commands, so a bloated live session can be inspected in the field without
// restarting under IKE_PPROF or sending SIGUSR1 by hand:
//
//   - diag.memoryStats toasts a one-line heap summary whose numbers tell a
//     real leak (HeapInuse growing) from runtime-retained pages (freed but
//     not returned to the OS — on macOS those inflate the process footprint
//     until memory pressure).
//   - diag.heapDump writes goroutine + heap profiles via diag.Dump and toasts
//     where they landed.

// MemoryStatsMsg toasts the current heap summary (diag.memoryStats, #1537).
type MemoryStatsMsg struct{}

// HeapDumpMsg writes goroutine + heap profiles (diag.heapDump, #1537).
type HeapDumpMsg struct{}

// memoryCommands builds the diag.* command family.
func memoryCommands() []plugin.Command {
	return []plugin.Command{
		appCommand("diag.memoryStats", "Memory Statistics", MemoryStatsMsg{}),
		appCommand("diag.heapDump", "Write Heap Dump", HeapDumpMsg{}),
	}
}

// showMemoryStats scavenges first — debug.FreeOSMemory forces a GC and hands
// freed pages back — so the summary reflects what the process actually needs,
// and reading the stats doubles as the manual "give memory back" action.
func (m Model) showMemoryStats() (tea.Model, tea.Cmd) {
	debug.FreeOSMemory()
	m.host.Notify(host.Info, diag.MemSummary())
	return m, nil
}

// writeHeapDump writes the profile pair and toasts the destination.
func (m Model) writeHeapDump() (tea.Model, tea.Cmd) {
	prefix, err := diag.Dump()
	if err != nil {
		m.host.Notify(host.Warn, "heap dump: "+err.Error())
		return m, nil
	}
	m.host.Notify(host.Info, "heap dump written: "+prefix+"-{goroutines.txt,heap.pprof}")
	return m, nil
}
