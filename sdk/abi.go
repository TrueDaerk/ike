package sdk

// abi.go owns the raw wasm boundary: the exports IKE calls, the imports the
// typed host functions (host.go) marshal through, and the guest allocator.
// Everything crosses as (ptr, len) regions of JSON bytes — see the ABI
// contract in the IKE repo (internal/wasm/abi).

import "unsafe"

//go:wasmimport ike open_file
func hostOpenFile(ptr, length uint32)

//go:wasmimport ike dispatch
func hostDispatch(ptr, length uint32)

//go:wasmimport ike notify
func hostNotify(ptr, length uint32)

//go:wasmimport ike set_status
func hostSetStatus(ptr, length uint32)

//go:wasmimport ike config_get
func hostConfigGet(ptr, length uint32) uint64

// buffers keeps every allocation reachable so the Go GC never reclaims
// memory the host still writes into or reads from.
var buffers [][]byte

//go:wasmexport ike_alloc
func ikeAlloc(size uint32) uint32 {
	buf := make([]byte, size)
	buffers = append(buffers, buf)
	return uint32(uintptr(unsafe.Pointer(&buf[0])))
}

//go:wasmexport register
func register() uint64 {
	ptr, length := regionOf(capsJSON())
	return uint64(ptr)<<32 | uint64(length)
}

//go:wasmexport on_command
func onCommand(ptr, length uint32) {
	dispatchCommand(string(readRegion(ptr, length)))
}

//go:wasmexport on_key
func onKey(ptr, length uint32) {
	dispatchKey(string(readRegion(ptr, length)))
}

//go:wasmexport on_hook
func onHook(ptr, length, pptr, plen uint32) {
	id := string(readRegion(ptr, length))
	payload := append([]byte(nil), readRegion(pptr, plen)...)
	dispatchHook(id, payload)
}

// regionOf exposes a byte slice as a (ptr, len) pair. The caller must keep b
// alive across the host call; all SDK call sites pass freshly built slices
// that stay live for the duration of the call.
func regionOf(b []byte) (uint32, uint32) {
	if len(b) == 0 {
		return 0, 0
	}
	return uint32(uintptr(unsafe.Pointer(&b[0]))), uint32(len(b))
}

// readRegion views a (ptr, len) region of this module's linear memory. The
// view aliases guest memory — copy before retaining. The uintptr→Pointer
// conversion trips vet's unsafeptr check but is sound here: on wasm the
// module's whole linear memory is one flat address space and ptr always
// originates from this module's own allocator.
func readRegion(ptr, length uint32) []byte {
	if length == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
}
