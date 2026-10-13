package capture

import "fmt"

// FormatSize renders a byte count for a person: the progress of a transfer,
// and the space a capture would need.
//
// It is here rather than in the CLI because quo reports sizes from more than
// one place and a reader should not have to work out that two of them mean
// the same thing.
func FormatSize(count int64) (rendered string) {
	const unit = 1024
	if count < unit {
		rendered = fmt.Sprintf("%d B", count)
		return
	}
	divisor := int64(unit)
	at := 0
	for remaining := count / unit; remaining >= unit; remaining /= unit {
		divisor *= unit
		at++
	}
	rendered = fmt.Sprintf("%.1f %ciB", float64(count)/float64(divisor), "KMGTPE"[at])
	return
}
