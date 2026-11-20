package agent

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func safeProjectComponent(part string) bool {
	lower := strings.ToLower(part)
	return !strings.HasPrefix(lower, ".") && !excluded[lower] &&
		!strings.HasSuffix(part, ".") && !strings.HasSuffix(part, " ") &&
		!strings.ContainsAny(part, "\\:\x00") && filepath.IsLocal(part)
}

// The workspace manager stores the real, absolute project directory at bind
// time. Reject a later substitution through symlinks or Windows junctions.
func openProjectRoot(name string) (*os.Root, error) {
	if !filepath.IsAbs(name) {
		return nil, fmt.Errorf("项目目录不可用")
	}
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil || filepath.Clean(name) != resolved {
		return nil, fmt.Errorf("项目目录不可用或已替换，请重新绑定")
	}
	info, err := os.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("项目目录不可用或为链接")
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("项目目录在打开时已变化")
	}
	return root, nil
}

// checkedDirectory rejects links at each component and compares the opened
// handle against the checked entry, including filesystem-native aliases.
func checkedDirectory(root *os.Root, name string) (*os.File, error) {
	if !fs.ValidPath(name) {
		return nil, fmt.Errorf("无效项目子目录")
	}
	var checked os.FileInfo
	var err error
	if name == "." {
		checked, err = root.Stat(".")
	} else {
		parts := strings.Split(name, "/")
		for i, part := range parts {
			if !safeProjectComponent(part) {
				return nil, fmt.Errorf("不允许此项目子目录")
			}
			checked, err = root.Lstat(strings.Join(parts[:i+1], "/"))
			if err != nil || !checked.IsDir() || checked.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("项目子目录不可用或为链接")
			}
		}
	}
	if err != nil {
		return nil, err
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	actual, err := file.Stat()
	if err != nil || !os.SameFile(checked, actual) {
		file.Close()
		return nil, fmt.Errorf("项目子目录在打开时已变化")
	}
	return file, nil
}
