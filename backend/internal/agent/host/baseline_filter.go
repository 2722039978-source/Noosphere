package host

import (
	"context"
	"io/fs"
	"os"
	"strings"
)

// Some real projects contain many gigabytes of explicitly ignored build and
// distribution output. Read only literal directory rules from the root
// .gitignore. File patterns never exclude untracked source or working edits.
// Keep directories with a negation below them to avoid losing included files.
func baselineFilters(ctx context.Context, root *os.Root) map[string]bool {
	rules := map[string]bool{}
	var keep []string
	b := &byteRange{limit: 256 << 10}
	v, err := inspect(ctx, root, ".gitignore", b)
	if err != nil || v.Size > 256<<10 || v.Hash == "" {
		return rules
	}
	for _, line := range strings.Split(string(b.data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "!") {
			keep = append(keep, strings.Trim(strings.TrimPrefix(line, "!"), "/"))
			continue
		}
		if !strings.HasSuffix(line, "/") || strings.HasPrefix(line, "#") || strings.ContainsAny(line, "*?[\\") {
			continue
		}
		p := strings.Trim(line, "/")
		if fs.ValidPath(p) && p != "." && eligible(p) {
			rules[p] = true
		}
	}
	for p := range rules {
		for _, n := range keep {
			if n == p || strings.HasPrefix(n, p+"/") || strings.ContainsAny(n, "*?[") {
				delete(rules, p)
				break
			}
		}
	}
	return rules
}
