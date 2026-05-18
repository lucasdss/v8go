// Package jit — sparkplug_shared: Shared types, offsets, and utilities for Sparkplug JIT.
// Platform-independent: used by both ARM64 and AMD64.
package jit

import (
	"unsafe"

	"github.com/lucasdss/v8go/pkg/js"
)

// Sparkplug is V8Go's baseline JIT compiler.
const (
	// PrologueSize is the number of bytes in the generated prologue.
	PrologueSize = 64
	// MaxObjectRegs is the maximum number of callee-saved registers that can
	// simultaneously hold object pointers.
	MaxObjectRegs = 7
)

// VMFrame / JSValue field offsets, computed once at package init.
var (
	accOffset    int
	accObjValOff int

	jsValueSize       int
	numValOff         int
	tagWordOff        int
	regsOff           int
	accTagAlign       int
	funcOff           int
	globalValsDataOff int
	inSparkplugOff    int
)

func init() {
	accOffset = int(unsafe.Offsetof(js.VMFrame{}.Acc))
	accObjValOff = int(unsafe.Offsetof(js.JSValue{}.ObjVal))

	jsValueSize = int(unsafe.Sizeof(js.JSValue{}))
	numValOff = int(unsafe.Offsetof(js.JSValue{}.NumVal))
	rawTagOff := int(unsafe.Offsetof(js.JSValue{}.Tag))
	tagWordOff = rawTagOff &^ 7
	regsOff = int(unsafe.Offsetof(js.VMFrame{}.Regs))
	accTagAlign = accOffset + tagWordOff
	funcOff = int(unsafe.Offsetof(js.VMFrame{}.Func))
	globalValsDataOff = int(unsafe.Offsetof(js.BytecodeFunction{}.GlobalVals))
	inSparkplugOff = int(unsafe.Offsetof(js.VMFrame{}.InSparkplug))
}

// FrameShadowStack is the per-frame shadow stack instance.
type FrameShadowStack = ShadowStack

// PrologueSetup initializes the shadow stack for a new JIT frame.
func PrologueSetup(frameShadowStack unsafe.Pointer) *ShadowStack {
	if frameShadowStack == nil {
		return nil
	}
	return (*ShadowStack)(frameShadowStack)
}

// EpilogueTeardown clears all shadow stack entries for the frame.
func EpilogueTeardown(ss *ShadowStack) {
	if ss != nil {
		ss.Clear()
	}
}

// SpillObjectPtr saves an object pointer to the shadow stack.
func SpillObjectPtr(ss *ShadowStack, objPtr unsafe.Pointer) {
	if ss != nil && objPtr != nil {
		ss.Push(objPtr)
	}
}

// SpillJSValue spills an object JSValue to the shadow stack.
func SpillJSValue(ss *ShadowStack, val js.JSValue) {
	if val.Tag == js.TagObject && val.ObjVal != nil {
		ss.Push(unsafe.Pointer(val.ObjVal)) //nolint:gosec // JIT shadow stack requires raw pointers
	}
}

// SpillAllJSValues spills all object values from a slice to the shadow stack.
func SpillAllJSValues(ss *ShadowStack, vals []js.JSValue) {
	if ss == nil {
		return
	}
	for i := range vals {
		SpillJSValue(ss, vals[i])
	}
}

// CodeBuf registry and RegisterCodeBuf are declared in icpatch.go.
// These are re-exported here to avoid import cycles between platform-specific files.
