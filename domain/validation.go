package domain

import "strings"

// Valid reports whether value is a valid lowercase ASCII domain name.
func Valid(value string) bool {
	if len(value) > 253 {
		return false
	}
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if !validLabel(part) {
			return false
		}
	}
	return true
}
