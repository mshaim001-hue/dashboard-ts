//go:build !darwin

package idle

import "time"

func systemIdle(_ time.Duration) bool {
	return false
}
