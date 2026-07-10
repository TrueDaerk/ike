package abi

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

// guest.go is the host-side view of the guest entry points: helpers that
// call the exported functions with the (ptr, len) conventions, plus the
// linear-memory read/write primitives they share.

// ReadRegion copies a (ptr, len) region out of the module's memory.
func ReadRegion(mod api.Module, ptr, length uint32) ([]byte, error) {
	if length == 0 {
		return nil, nil
	}
	b, ok := mod.Memory().Read(ptr, length)
	if !ok {
		return nil, fmt.Errorf("abi: read %d bytes at %d out of range", length, ptr)
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out, nil
}

// AllocAndWrite places data into guest memory through the module's exported
// allocator and returns the packed (ptr, len) region.
func AllocAndWrite(ctx context.Context, mod api.Module, data []byte) (uint64, error) {
	if len(data) == 0 {
		return 0, nil
	}
	alloc := mod.ExportedFunction(ExportAlloc)
	if alloc == nil {
		return 0, fmt.Errorf("abi: module %q exports no %s", mod.Name(), ExportAlloc)
	}
	res, err := alloc.Call(ctx, uint64(len(data)))
	if err != nil {
		return 0, fmt.Errorf("abi: %s: %w", ExportAlloc, err)
	}
	ptr := uint32(res[0])
	if !mod.Memory().Write(ptr, data) {
		return 0, fmt.Errorf("abi: write %d bytes at %d out of range", len(data), ptr)
	}
	return PackPtrLen(ptr, uint32(len(data))), nil
}

// Register calls the guest's register() and decodes its capability payload.
// A module without the export contributes nothing (nil, nil).
func Register(ctx context.Context, mod api.Module) (*Capabilities, error) {
	fn := mod.ExportedFunction(ExportRegister)
	if fn == nil {
		return nil, nil
	}
	res, err := fn.Call(ctx)
	if err != nil {
		return nil, fmt.Errorf("abi: register: %w", err)
	}
	ptr, length := UnpackPtrLen(res[0])
	data, err := ReadRegion(mod, ptr, length)
	if err != nil {
		return nil, err
	}
	caps, err := DecodeCapabilities(data)
	if err != nil {
		return nil, err
	}
	return &caps, nil
}

// CallCommand invokes on_command with the command id.
func CallCommand(ctx context.Context, mod api.Module, id string) error {
	return callWithBytes(ctx, mod, ExportOnCommand, []byte(id))
}

// CallKey invokes on_key with the binding's command id.
func CallKey(ctx context.Context, mod api.Module, id string) error {
	return callWithBytes(ctx, mod, ExportOnKey, []byte(id))
}

// CallHook invokes on_hook with the hook id and its payload.
func CallHook(ctx context.Context, mod api.Module, id string, payload []byte) error {
	fn := mod.ExportedFunction(ExportOnHook)
	if fn == nil {
		return fmt.Errorf("abi: module %q exports no %s", mod.Name(), ExportOnHook)
	}
	idRegion, err := AllocAndWrite(ctx, mod, []byte(id))
	if err != nil {
		return err
	}
	payloadRegion, err := AllocAndWrite(ctx, mod, payload)
	if err != nil {
		return err
	}
	idPtr, idLen := UnpackPtrLen(idRegion)
	pPtr, pLen := UnpackPtrLen(payloadRegion)
	_, err = fn.Call(ctx, uint64(idPtr), uint64(idLen), uint64(pPtr), uint64(pLen))
	if err != nil {
		return fmt.Errorf("abi: %s: %w", ExportOnHook, err)
	}
	return nil
}

// callWithBytes writes data into guest memory and calls export(ptr, len).
func callWithBytes(ctx context.Context, mod api.Module, export string, data []byte) error {
	fn := mod.ExportedFunction(export)
	if fn == nil {
		return fmt.Errorf("abi: module %q exports no %s", mod.Name(), export)
	}
	region, err := AllocAndWrite(ctx, mod, data)
	if err != nil {
		return err
	}
	ptr, length := UnpackPtrLen(region)
	if _, err := fn.Call(ctx, uint64(ptr), uint64(length)); err != nil {
		return fmt.Errorf("abi: %s: %w", export, err)
	}
	return nil
}
