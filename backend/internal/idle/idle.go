package idle

import "time"

// IsIdle returns true if user idle longer than threshold (default 60s like dashboard).
func IsIdle(threshold time.Duration) bool {
	if threshold <= 0 {
		threshold = 60 * time.Second
	}
	return systemIdle(threshold)
}
