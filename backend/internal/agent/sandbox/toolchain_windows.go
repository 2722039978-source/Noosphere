package sandbox

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Some language runtimes initialize Winsock even while working offline. RegistryRead
// allows the OS runtime to discover its providers; no network capability is added.
func registryCapability() (*windows.SID, error) {
	name, _ := windows.UTF16PtrFromString("registryRead")
	var groups, capabilities **windows.SID
	var groupCount, capCount uint32
	proc := windows.NewLazySystemDLL("kernelbase.dll").NewProc("DeriveCapabilitySidsFromName")
	if e := proc.Find(); e != nil {
		return nil, e
	}
	ok, _, err := proc.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&groups)), uintptr(unsafe.Pointer(&groupCount)), uintptr(unsafe.Pointer(&capabilities)), uintptr(unsafe.Pointer(&capCount)))
	if ok == 0 {
		return nil, err
	}
	free := func(list **windows.SID, n uint32) {
		for _, sid := range unsafe.Slice(list, n) {
			windows.LocalFree(windows.Handle(unsafe.Pointer(sid)))
		}
		windows.LocalFree(windows.Handle(unsafe.Pointer(list)))
	}
	defer free(groups, groupCount)
	defer free(capabilities, capCount)
	if capCount < 1 {
		return nil, fmt.Errorf("registryRead capability unavailable")
	}
	return (*capabilities).Copy()
}

// Copy only the executable distribution, never the parent installation ACL or a user's package cache.
func privateToolchain(ctx context.Context, name, exe, dir string) (string, []string, error) {
	if name == browserRenderer {
		return stageHTMLRenderer(ctx, exe, dir)
	}
	if name == "cmd" || name == "powershell" {
		return exe, nil, nil
	}
	root := filepath.Dir(exe)
	relative := filepath.Base(exe)
	if name == "git" {
		root = filepath.Dir(root)
		candidate := filepath.Join(root, "mingw64", "bin", "git.exe")
		if _, e := os.Stat(candidate); e == nil {
			root = filepath.Dir(candidate)
			relative = "git.exe"
		} else {
			return "", nil, fmt.Errorf("需要 Git for Windows 的 mingw64 运行时")
		}
	}
	if name == "go" {
		root = filepath.Dir(root)
		relative = "bin/go.exe"
	}
	allow := func(rel string, isDir bool) bool {
		p := strings.ToLower(filepath.ToSlash(rel))
		if strings.Contains(p, "site-packages") || strings.Contains(p, "__pycache__") {
			return false
		}
		if name == "npm" {
			return p == "node_modules" || p == "node_modules/npm" || strings.HasPrefix(p, "node_modules/npm/") || (!isDir && (p == "node.exe" || p == "npm.cmd" || p == "npx.cmd" || (!strings.Contains(p, "/") && strings.HasSuffix(p, ".dll"))))
		}
		if isDir {
			return (name == "python" && (p == "lib" || strings.HasPrefix(p, "lib/") || p == "dlls" || strings.HasPrefix(p, "dlls/"))) || (name == "go" && (p == "bin" || p == "src" || p == "pkg" || p == "lib" || strings.HasPrefix(p, "src/") || strings.HasPrefix(p, "pkg/") || strings.HasPrefix(p, "lib/")))
		}
		if p == strings.ToLower(filepath.ToSlash(relative)) {
			return true
		}
		switch name {
		case "git", "node":
			return !strings.Contains(p, "/") && strings.HasSuffix(p, ".dll")
		case "python":
			return (!strings.Contains(p, "/") && strings.HasSuffix(p, ".dll")) || strings.HasPrefix(p, "lib/") || strings.HasPrefix(p, "dlls/")
		case "go":
			return strings.HasPrefix(p, "src/") || strings.HasPrefix(p, "pkg/") || strings.HasPrefix(p, "lib/") || p == "version" || p == "go.env"
		}
		return false
	}
	target := filepath.Join(dir, "tools")
	if e := os.Mkdir(target, 0700); e != nil {
		return "", nil, e
	}
	var size int64
	e := filepath.WalkDir(root, func(path string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rel, e := filepath.Rel(root, path)
		if e != nil || rel == "." {
			return e
		}
		if !allow(rel, entry.IsDir()) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		dest := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.Mkdir(dest, 0700)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		defer f.Close()
		sizeInfo, e := f.Stat()
		if e != nil {
			return e
		}
		size += sizeInfo.Size()
		if size > 2<<30 {
			return fmt.Errorf("工具链超过 2 GiB")
		}
		o, e := os.OpenFile(dest, os.O_WRONLY|os.O_EXCL|os.O_CREATE, 0600)
		if e != nil {
			return e
		}
		_, e = io.Copy(o, f)
		closeErr := o.Close()
		if e != nil {
			return e
		}
		return closeErr
	})
	if e != nil {
		return "", nil, e
	}
	var env []string
	switch name {
	case "npm":
		env = []string{"NODE_OPTIONS=--preserve-symlinks --preserve-symlinks-main", "NPM_CONFIG_OFFLINE=true", "NPM_CONFIG_UPDATE_NOTIFIER=false", "NPM_CONFIG_AUDIT=false", "NPM_CONFIG_FUND=false"}
	case "go":
		env = []string{"GOROOT=" + target, "GOTELEMETRY=off"}
	case "python":
		env = []string{"PYTHONNOUSERSITE=1", "PYTHONHOME=" + target}
	}
	return filepath.Join(target, filepath.FromSlash(relative)), env, nil
}
