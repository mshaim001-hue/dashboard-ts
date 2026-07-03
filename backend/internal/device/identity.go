package device

import (
	"fmt"
)

// UniqueHostname returns E3-NN not present in used (campus-style desk label for heartbeat).
func UniqueHostname(used map[string]bool) string {
	for n := 1; n <= 99; n++ {
		h := fmt.Sprintf("E3-%02d", n)
		if !used[h] {
			return h
		}
	}
	for n := 100; n <= 199; n++ {
		h := fmt.Sprintf("E3-%d", n)
		if !used[h] {
			return h
		}
	}
	return "E3-XX"
}

// DeviceName is sent in heartbeat start payloads.
func DeviceName(hostname string) string {
	return hostname + " · ts-tracker"
}
