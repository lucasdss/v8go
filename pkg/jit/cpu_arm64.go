//go:build arm64

package jit

import (
	"runtime"

	"golang.org/x/sys/unix"
)

// hasARM64PAC reports whether the CPU supports ARM Pointer Authentication
// Codes (FEAT_PAuth / ARMv8.3+). PAC instructions (paciasp/autiasp) sign
// and authenticate return addresses to prevent ROP attacks.
//
// Detection strategy:
//   - Darwin ARM64: all Apple Silicon chips (M1+) implement ARMv8.5 with PAC.
//   - Linux ARM64: check HWCAP_PACA (bit 30) from the ELF auxiliary vector.
//   - Other ARM64: disabled until detection is implemented.
var hasARM64PAC bool

func init() {
	switch runtime.GOOS {
	case "darwin":
		// Apple Silicon always has PAC.
		hasARM64PAC = true
	case "linux":
		hasARM64PAC = checkLinuxPAC()
	default:
		hasARM64PAC = false
	}
}

// checkLinuxPAC checks for pointer authentication via the ELF auxiliary vector.
// HWCAP_PACA (1<<30) indicates the PACIA instruction is available.
// HWCAP_PACG (1<<31) indicates the generic PAC instructions are available.
func checkLinuxPAC() bool {
	auxv, err := unix.Auxv()
	if err != nil {
		return false
	}
	const (
		AT_HWCAP    = 16
		HWCAP_PACA  = 1 << 30
		HWCAP_PACG  = 1 << 31
	)
	for _, entry := range auxv {
		if entry[0] == AT_HWCAP {
			return uintptr(entry[1])&(HWCAP_PACA|HWCAP_PACG) != 0
		}
	}
	return false
}
