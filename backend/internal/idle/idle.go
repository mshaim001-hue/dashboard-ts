package idle

import "time"

var detectIdle bool

// SetDetectIdle enables macOS HID idle detection (off by default).
func SetDetectIdle(on bool) {
	detectIdle = on
}

// ForHeartbeat reports whether the school heartbeat should use idle=true.
func ForHeartbeat() bool {
	if !detectIdle {
		return false
	}
	return IsIdle(60 * time.Second)
}

// IsIdle returns true if user idle longer than threshold (default 60s like dashboard).
func IsIdle(threshold time.Duration) bool {
	if threshold <= 0 {
		threshold = 60 * time.Second
	}
	return systemIdle(threshold)
}
