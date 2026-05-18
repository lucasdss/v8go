// alloc.go — Bump allocator for VMFrame, JSValue registers, and JSObject.
//
// The Allocator pre-allocates fixed-capacity buffers to avoid heap allocations
// during bytecode execution. Frames and registers use LIFO bump allocation;
// objects try the bump buffer first, then fall back to a sync.Pool.
package js

import (
	"sync"
	"unsafe"
)

// allocFrameBufSize is the number of pre-allocated VMFrame slots in the
// Allocator's frame buffer. 64 slots covers typical call depth without heap
// allocation.
const allocFrameBufSize = 64

// Allocator pre-allocates buffers for frames, registers, and objects to
// minimize GC pressure during bytecode execution. All allocation methods
// are designed for LIFO usage patterns (child frames freed before parents).
//
// The Allocator is NOT goroutine-safe — the caller (VM.mu) provides
// synchronization.
type Allocator struct {
	frameBuf  []VMFrame
	frameUsed int

	regBuf  []JSValue
	regUsed int

	objBuf  []JSObject
	objUsed int

	objPool sync.Pool
}

// NewAllocator creates an Allocator with pre-allocated buffers.
func NewAllocator() *Allocator {
	a := &Allocator{
		frameBuf: make([]VMFrame, allocFrameBufSize),
		regBuf:   make([]JSValue, 256*8),
		objBuf:   make([]JSObject, 64),
	}
	a.objPool = sync.Pool{
		New: func() interface{} { return &JSObject{} },
	}
	return a
}

// AllocFrame obtains a VMFrame from the pre-allocated buffer.
// Falls back to heap allocation if the buffer is exhausted.
// Must be paired with FreeFrame.
func (a *Allocator) AllocFrame() *VMFrame {
	if a.frameUsed < len(a.frameBuf) {
		f := &a.frameBuf[a.frameUsed]
		a.frameUsed++
		*f = VMFrame{} // zero out stale fields
		return f
	}
	return &VMFrame{}
}

// FreeFrame releases a VMFrame. For buffer-allocated frames, rewinds the
// bump allocator (frames are LIFO: child frames always freed before parents).
// Heap-allocated frames are left for GC.
func (a *Allocator) FreeFrame(f *VMFrame) {
	if f.GenState != nil {
		return
	}
	if len(a.frameBuf) == 0 {
		return
	}
	frameSize := int(unsafe.Sizeof(VMFrame{}))
	fPtr := uintptr(unsafe.Pointer(f))
	bufBase := uintptr(unsafe.Pointer(&a.frameBuf[0]))
	bufEnd := bufBase + uintptr(len(a.frameBuf))*uintptr(frameSize)
	if fPtr >= bufBase && fPtr < bufEnd {
		offset := int((fPtr - bufBase) / uintptr(frameSize))
		if offset+1 == a.frameUsed {
			a.frameUsed = offset
		}
	}
}

// AllocRegs obtains a []JSValue slice of at least n elements from the
// pre-allocated register buffer. Falls back to make() for large register files.
func (a *Allocator) AllocRegs(n int) []JSValue {
	if n <= 256 && a.regUsed+n <= len(a.regBuf) {
		start := a.regUsed
		a.regUsed += n
		regs := a.regBuf[start:a.regUsed]
		for i := range regs {
			regs[i] = JSValue{}
		}
		return regs
	}
	return make([]JSValue, n)
}

// FreeRegs releases a register slice. For buffer-allocated slices, rewinds
// the bump allocator (registers are LIFO: child freed before parent).
// Heap-allocated slices are left for GC.
func (a *Allocator) FreeRegs(regs []JSValue) {
	if len(regs) == 0 || len(a.regBuf) == 0 {
		return
	}
	regPtr := uintptr(unsafe.Pointer(&regs[0]))
	bufBase := uintptr(unsafe.Pointer(&a.regBuf[0]))
	elemSize := unsafe.Sizeof(JSValue{})
	bufEnd := bufBase + uintptr(len(a.regBuf))*elemSize
	if regPtr >= bufBase && regPtr < bufEnd {
		offset := int((regPtr - bufBase) / elemSize)
		if offset+len(regs) == a.regUsed {
			a.regUsed = offset
		}
	}
}

// AllocObj obtains a *JSObject from the pre-allocated object buffer.
// Falls back to the sync.Pool when the buffer is exhausted.
func (a *Allocator) AllocObj() *JSObject {
	if a.objUsed < len(a.objBuf) {
		obj := &a.objBuf[a.objUsed]
		a.objUsed++
		*obj = JSObject{}
		return obj
	}
	obj := a.objPool.Get().(*JSObject)
	*obj = JSObject{}
	obj.Shape = EmptyShape
	obj.Prototype = ObjectPrototype
	obj.ConstructorName = "Object"
	return obj
}

// FreeObj releases a JSObject. For buffer-allocated objects, rewinds the
// bump allocator. Pool-allocated objects are returned to the pool.
func (a *Allocator) FreeObj(obj *JSObject) {
	// Reset mixin pointers before returning to pool.
	obj.interceptor = nil
	obj.proxy = nil
	obj.typedArray = nil
	obj.generator = nil
	obj.flags = 0
	if len(a.objBuf) == 0 || a.objUsed == 0 {
		a.objPool.Put(obj)
		return
	}
	objPtr := uintptr(unsafe.Pointer(obj))
	bufBase := uintptr(unsafe.Pointer(&a.objBuf[0]))
	elemSize := unsafe.Sizeof(JSObject{})
	bufEnd := bufBase + uintptr(len(a.objBuf))*elemSize
	if objPtr >= bufBase && objPtr < bufEnd {
		offset := int((objPtr - bufBase) / elemSize)
		if offset+1 == a.objUsed {
			a.objUsed = offset
		}
		return
	}
	a.objPool.Put(obj)
}

// Reset rewinds all bump allocators to zero. Called at the start of each
// top-level execute() call.
func (a *Allocator) Reset() {
	a.frameUsed = 0
	a.regUsed = 0
	a.objUsed = 0
}
