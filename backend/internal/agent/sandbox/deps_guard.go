package sandbox

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func isPathWithin(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func isStrictRealDir(info os.FileInfo) bool {
	return info != nil && info.Mode().Type() == fs.ModeDir
}

func canonicalRealDir(dir string) (string, bool) {
	if dir == "" || !filepath.IsAbs(dir) {
		return "", false
	}
	cleaned := filepath.Clean(dir)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", false
	}
	info, err := os.Lstat(resolved)
	if err != nil || !isStrictRealDir(info) {
		return "", false
	}
	return resolved, true
}

// safeContainedRealDir walks every path segment of rel inside rootDir via os.Root,
// verifying that no intermediate directory or leaf is a symlink/reparse point and
// that the resolved canonical directory remains strictly inside rootDir.
func safeContainedRealDir(rootDir, rel string) (string, bool) {
	canonicalRoot, ok := canonicalRealDir(rootDir)
	if !ok {
		return "", false
	}
	slash := path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	if slash == "." || !fs.ValidPath(slash) || strings.ContainsAny(slash, "\\:\x00") {
		return "", false
	}
	root, err := os.OpenRoot(canonicalRoot)
	if err != nil {
		return "", false
	}
	defer root.Close()
	cur, err := root.OpenRoot(".")
	if err != nil {
		return "", false
	}
	defer func() {
		if cur != nil {
			_ = cur.Close()
		}
	}()
	for _, part := range strings.Split(slash, "/") {
		if !filepath.IsLocal(part) {
			return "", false
		}
		entry, err := cur.Lstat(part)
		if err != nil || !isStrictRealDir(entry) {
			return "", false
		}
		next, err := cur.OpenRoot(part)
		if err != nil {
			return "", false
		}
		actual, statErr := next.Stat(".")
		if statErr != nil || !os.SameFile(entry, actual) {
			_ = next.Close()
			return "", false
		}
		_ = cur.Close()
		cur = next
	}
	candidate := filepath.Join(canonicalRoot, filepath.FromSlash(slash))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(candidate) {
		return "", false
	}
	if !isPathWithin(canonicalRoot, resolved) {
		return "", false
	}
	info, err := os.Lstat(resolved)
	if err != nil || !isStrictRealDir(info) {
		return "", false
	}
	return resolved, true
}

// safeContainedChildPath verifies that relDir (which may be ".") is a real,
// symlink-free directory inside rootDir, and that childName is a safe single
// path segment that does not currently exist inside that directory.
func safeContainedChildPath(rootDir, relDir, childName string) (string, bool) {
	if childName == "" || strings.ContainsAny(childName, "/\\:\x00") || !filepath.IsLocal(childName) {
		return "", false
	}
	parent := ""
	slash := path.Clean(strings.ReplaceAll(relDir, "\\", "/"))
	if slash == "." || slash == "" {
		var ok bool
		parent, ok = canonicalRealDir(rootDir)
		if !ok {
			return "", false
		}
	} else {
		var ok bool
		parent, ok = safeContainedRealDir(rootDir, slash)
		if !ok {
			return "", false
		}
	}
	child := filepath.Join(parent, childName)
	if _, err := os.Lstat(child); !os.IsNotExist(err) {
		return "", false
	}
	return child, true
}
