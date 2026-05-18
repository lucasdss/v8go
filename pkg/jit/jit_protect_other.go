//go:build !darwin

package jit

// jitWriteProtect is a no-op on non-Darwin platforms.
// With true dual-mapping (memfd_create on Linux), writes go through the
// RW mapping and execution through the RX mapping simultaneously — no
// protection toggle is needed.
func jitWriteProtect(enabled bool) {}
