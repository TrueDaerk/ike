//go:build darwin

package terminal

import (
	"syscall"
	"unsafe"
)

// processCwd asks the kernel for pid's current working directory via
// proc_pidinfo(pid, PROC_PIDVNODEPATHINFO) — the same query libproc's
// proc_pidinfo() wraps, issued as a raw SYS_PROC_INFO syscall so the build
// stays cgo-free. Returns "" when the query fails (process gone, sandbox
// denial); callers fall back to the start directory.
const (
	sysProcInfo          = 336 // SYS_PROC_INFO
	procInfoCallPidInfo  = 2   // PROC_INFO_CALL_PIDINFO
	procPidVnodePathInfo = 9   // PROC_PIDVNODEPATHINFO
)

// procVnodePathInfo mirrors struct proc_vnodepathinfo from <sys/proc_info.h>:
// two vnode_info_path entries (cwd, chroot root), each a 152-byte vnode_info
// followed by a MAXPATHLEN (1024) path buffer.
type procVnodePathInfo struct {
	_        [152]byte
	cdirPath [1024]byte
	_        [152]byte
	_        [1024]byte
}

func processCwd(pid int) string {
	var vpi procVnodePathInfo
	n, _, errno := syscall.Syscall6(sysProcInfo,
		procInfoCallPidInfo, uintptr(pid), procPidVnodePathInfo,
		0, uintptr(unsafe.Pointer(&vpi)), unsafe.Sizeof(vpi))
	if errno != 0 || n != unsafe.Sizeof(vpi) {
		return ""
	}
	for i, c := range vpi.cdirPath {
		if c == 0 {
			return string(vpi.cdirPath[:i])
		}
	}
	return ""
}
