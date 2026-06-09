package output

import "fmt"

// HumanSize renders a byte count as a compact human-readable string
// (e.g. 5 → "5 B", 4412345 → "4.2 MB"). It uses binary (1024) units.
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
