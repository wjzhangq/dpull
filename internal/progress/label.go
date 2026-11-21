package progress

import "strings"

// ShortLabel is the name shown beside a progress bar.
// Docker digests keep an 8-character hash. Any other id is shown as a file name.
func ShortLabel(id string) string {
	const prefix = "sha256:"
	if strings.HasPrefix(id, prefix) {
		hexPart := id[len(prefix):]
		if len(hexPart) > 8 {
			return hexPart[:8]
		}
		return hexPart
	}
	base := id
	if i := strings.LastIndexAny(id, `/\`); i >= 0 && i+1 < len(id) {
		base = id[i+1:]
	}
	if len(base) > 40 {
		return base[:37] + "..."
	}
	return base
}
