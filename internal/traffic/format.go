package traffic

import "fmt"

// FormatBytes renders n with 1024-based units: "1023 B", "12.3 KB", "1.5 MB".
func FormatBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, units := float64(n)/unit, []string{"KB", "MB", "GB", "TB"}
	i := 0
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// FormatRate renders a bytes-per-second rate, e.g. "12.3 KB/s".
func FormatRate(bps uint64) string { return FormatBytes(bps) + "/s" }
