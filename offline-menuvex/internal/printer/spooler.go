package printer

import (
	"sort"
	"strings"
)

// ParseQueueList normalizes installed-queue output (lpstat -e / Get-Printer)
// and removes duplicates while dropping unsafe names.
func ParseQueueList(raw string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || seen[name] || ValidateQueueName(name) != nil {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
