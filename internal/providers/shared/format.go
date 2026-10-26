package shared

import "fmt"

// FormatTokenCountF returns a human-readable string for a token count
// (e.g. "1.5K", "2.3M", "1.0B").
func FormatTokenCountF(value float64) string {
	switch {
	case value >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", value/1_000_000_000)
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", value/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fK", value/1_000)
	default:
		return fmt.Sprintf("%.0f", value)
	}
}
