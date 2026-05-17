package jit

import (
	"math"
	"testing"
	"unsafe"

	"github.com/lucasdss/v8go/pkg/js"
)

// FuzzAssembler feeds random instruction sequences to the assembler.
func FuzzAssembler(f *testing.F) {
	f.Add([]byte{0xD5, 0x03, 0x20, 0x1F}) // NOP
	f.Add([]byte{0xC0, 0x03, 0x5F, 0xD6}) // RET

	f.Fuzz(func(t *testing.T, data []byte) {
		bufSize := 256
		buf, err := NewCodeBuf(bufSize)
		if err != nil {
			t.Skipf("NewCodeBuf: %v", err)
		}

		as := NewAssembler(buf)
		// Feed 4-byte chunks as raw instructions, staying within buffer bounds.
		for i := 0; i+3 < len(data) && as.Pos()+4 <= bufSize; i += 4 {
			val := uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16 | uint32(data[i+3])<<24
			as.buf.WriteUint32LE(val)
		}

		_ = as.Pos()
	})
}

// FuzzICSlotPatching feeds random parameters to IC slot patching.
func FuzzICSlotPatching(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 16 {
			return
		}
		buf, err := NewCodeBuf(64)
		if err != nil {
			t.Skipf("NewCodeBuf: %v", err)
		}

		seed := uint64(data[0]) | uint64(data[1])<<8 | uint64(data[2])<<16 | uint64(data[3])<<24 |
			uint64(data[4])<<32 | uint64(data[5])<<40 | uint64(data[6])<<48 | uint64(data[7])<<56

		shapePtr := uintptr(seed & 0xFFFFFFFF)
		propOffset := int(data[8]) % 128
		objShapeOffset := int(data[9])%128 + 16
		slotOffset := int(data[10]) % 32

		// Fill with NOPs.
		for i := 0; i < 64; i += 4 {
			buf.PatchUint32LE(i, 0xD503201F)
		}

		// All of these must not panic.
		PatchICSlot(buf, slotOffset, shapePtr, propOffset, objShapeOffset)
		PatchICSlotStore(buf, slotOffset, shapePtr, propOffset, objShapeOffset)
		PatchMegamorphicICSlot(buf, slotOffset)
	})
}

// FuzzSparkplugHelpers fuzzes sparkplug helper functions with random inputs.
func FuzzSparkplugHelpers(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 8 {
			return
		}

		numRegs := int(data[0]%64) + 1
		regIdx := int(data[1]) % numRegs
		accTag := data[2] % 8
		regTag := data[3] % 8
		accVal := math.Float64frombits(uint64(data[4]) | uint64(data[5])<<8 | uint64(data[6])<<16 | uint64(data[7])<<24)

		frame := &js.VMFrame{
			Func: &js.BytecodeFunction{
				Name:         "fuzz",
				NumRegisters: numRegs,
				Instructions: []js.Instruction{
					{Op: js.OpReturn},
				},
			},
			Regs: make([]js.JSValue, numRegs),
		}

		// Set accumulator based on tag.
		switch accTag {
		case 0:
			frame.Acc = js.NewNumber(accVal)
		case 1:
			frame.Acc = js.NewString("fuzz")
		case 2:
			frame.Acc = js.True
		case 3:
			frame.Acc = js.False
		case 4:
			frame.Acc = js.Undefined
		case 5:
			frame.Acc = js.Null
		default:
			frame.Acc = js.NewNumber(accVal)
		}

		// Set reg value.
		switch regTag {
		case 0:
			frame.Regs[regIdx] = js.NewNumber(accVal + 1)
		case 1:
			frame.Regs[regIdx] = js.NewString("bar")
		case 2:
			frame.Regs[regIdx] = js.True
		default:
			frame.Regs[regIdx] = js.NewNumber(accVal + 1)
		}

		// All helpers must not panic.
		sparkplugOpLdaSmi(frame, int8(data[0]))
		sparkplugOpLdaZero(frame)
		sparkplugOpLdaOne(frame)
		sparkplugOpLdaUndefined(frame)
		sparkplugOpLdaNull(frame)
		sparkplugOpLdaTrue(frame)
		sparkplugOpLdaFalse(frame)
		sparkplugOpStar(frame, regIdx)
		sparkplugOpLdar(frame, regIdx)
		sparkplugOpReturn(frame)
		sparkplugOpAdd(frame, regIdx)
		sparkplugOpSub(frame, regIdx)
		sparkplugOpMul(frame, regIdx)
		sparkplugOpDiv(frame, regIdx)
		sparkplugIsFalsy(frame)
		sparkplugOpStrictEq(frame, regIdx)
		sparkplugOpLessThan(frame, regIdx)
		sparkplugOpGreaterThan(frame, regIdx)
	})
}

// FuzzGCBridge fuzzes GC bridge registration and lookup.
func FuzzGCBridge(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 16 {
			return
		}

		start := uintptr(data[0]) | uintptr(data[1])<<8 | uintptr(data[2])<<16 | uintptr(data[3])<<24 |
			uintptr(data[4])<<32 | uintptr(data[5])<<40 | uintptr(data[6])<<48 | uintptr(data[7])<<56
		end := start + uintptr(data[8])%0x10000 + 0x1000
		pc := start + uintptr(data[9])%uintptr(end-start)

		regionMu.Lock()
		prev := registeredRegionSlice
		registeredRegionSlice = nil
		regionMu.Unlock()

		defer func() {
			regionMu.Lock()
			registeredRegionSlice = prev
			regionMu.Unlock()
		}()

		// Register up to 5 regions deterministically from the data.
		for i := 0; i < 5; i++ {
			RegisterRegion(Region{
				Start: start + uintptr(i)*0x10000,
				End:   end + uintptr(i)*0x10000,
			})
		}

		// These must not panic.
		NumRegions()
		findRegion(pc)
		NextJITFrame(pc, 0)
		ScanJITStack(pc, 0, func(unsafe.Pointer) {})
		PreemptJIT()

		// Unregister a range.
		UnregisterRegion(start, end)
	})
}
