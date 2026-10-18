package agent

import (
	"fmt"
	"math"
	"strings"
)

func validateToolArgs(name string, args map[string]any) error {
	var required, allowed []string
	switch name {
	case "system_info", "tasklist", "netstat", "service_list":
	case "service_status":
		required = []string{"name"}
		allowed = required
	case "tail_log":
		required = []string{"path"}
		allowed = []string{"path", "lines"}
	case "search_log":
		required = []string{"path", "keyword"}
		allowed = required
	default:
		return fmt.Errorf("unknown or unsupported diagnostic tool: %s", name)
	}
	for _, key := range required {
		value, ok := args[key].(string)
		if !ok || strings.TrimSpace(value) == "" || len(value) > 4096 {
			return fmt.Errorf("%s requires a nonempty %s", name, key)
		}
	}
	for key := range args {
		ok := false
		for _, candidate := range allowed {
			if key == candidate {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("unsupported argument %s", key)
		}
	}
	if value, ok := args["lines"]; ok {
		n, ok := value.(float64)
		if !ok || n < 1 || n > 200 || math.Trunc(n) != n {
			return fmt.Errorf("lines must be an integer from 1 to 200")
		}
	}
	return nil
}
