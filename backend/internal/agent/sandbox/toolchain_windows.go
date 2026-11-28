package sandbox

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const toolchainCacheSchema = "v2"

var toolchainCacheMu sync.Mutex

func namedCapability(capName string) (*windows.SID, error) {
	name, _ := windows.UTF16PtrFromString(capName)
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
		return nil, fmt.Errorf("%s capability unavailable", capName)
	}
	return (*capabilities).Copy()
}

// Some language runtimes initialize Winsock even while working offline. RegistryRead
// allows the OS runtime to discover its providers; no network capability is added.
func registryCapability() (*windows.SID, error) {
	return namedCapability("registryRead")
}

// A dedicated named capability SID grants read-only access to the shared toolchain
// and dependency caches without adding per-run SIDs to cached directory DACLs.
func readonlyCapability() (*windows.SID, error) {
	return namedCapability("noosphereAgentToolchainRead")
}

func resolveCacheBase(limitsCacheDir string) string {
	if strings.TrimSpace(limitsCacheDir) != "" {
		return filepath.Clean(limitsCacheDir)
	}
	return filepath.Join(os.TempDir(), "noosphere-agent-toolchain-cache")
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func isSensitiveName(p string) bool {
	lower := strings.ToLower(p)
	if strings.Contains(lower, "credential") || strings.Contains(lower, "secret") || strings.HasSuffix(lower, "/.env") || lower == ".env" {
		return true
	}
	switch strings.ToLower(filepath.Ext(lower)) {
	case ".pem", ".p12", ".pfx", ".key", ".kdbx":
		return true
	}
	return false
}

func toolchainGroup(name string) string {
	switch name {
	case "python", "python3", "pytest":
		return "python"
	case "npm", "npx":
		return "npm"
	case "cargo", "rustc":
		return "rust"
	default:
		return name
	}
}

func toolchainRootAndRel(ctx context.Context, group, exe string) (string, string, error) {
	root := filepath.Dir(exe)
	relative := filepath.Base(exe)
	switch group {
	case "git":
		// Git for Windows can put either its launcher or its actual
		// mingw64/bin executable on PATH.
		if !strings.EqualFold(filepath.Base(root), "bin") || !strings.EqualFold(filepath.Base(filepath.Dir(root)), "mingw64") {
			candidate := filepath.Join(filepath.Dir(root), "mingw64", "bin", "git.exe")
			if info, err := os.Stat(candidate); err != nil || !info.Mode().IsRegular() {
				return "", "", fmt.Errorf("需要 Git for Windows 的 mingw64 运行时")
			}
			root = filepath.Dir(candidate)
			relative = "git.exe"
		}
	case "go":
		root = filepath.Dir(root)
		relative = "bin/go.exe"
	case "rust":
		if rustcPath, err := exec.LookPath("rustc.exe"); err == nil {
			if out, err := exec.CommandContext(ctx, rustcPath, "--print", "sysroot").Output(); err == nil {
				sysroot := strings.TrimSpace(string(out))
				if sysroot != "" {
					if _, e := os.Stat(filepath.Join(sysroot, "bin", filepath.Base(exe))); e == nil {
						root = sysroot
						relative = "bin/" + filepath.Base(exe)
					}
				}
			}
		}
	}
	return root, relative, nil
}

func copyToolchainTree(ctx context.Context, group, root, relative, target string) error {
	allow := func(rel string, isDir bool) bool {
		p := strings.ToLower(filepath.ToSlash(rel))
		if strings.Contains(p, "__pycache__") || strings.HasPrefix(p, ".git/") || p == ".git" || isSensitiveName(p) {
			return false
		}
		if group == "go" {
			if strings.HasSuffix(p, "_test.go") || strings.Contains(p, "/testdata/") || strings.HasSuffix(p, "/testdata") || p == "test" || strings.HasPrefix(p, "test/") || p == "doc" || strings.HasPrefix(p, "doc/") || p == "misc" || strings.HasPrefix(p, "misc/") {
				return false
			}
		}
		if group == "npm" {
			return p == "node_modules" || p == "node_modules/npm" || strings.HasPrefix(p, "node_modules/npm/") || (!isDir && (p == "node.exe" || p == "npm.cmd" || p == "npx.cmd" || (!strings.Contains(p, "/") && strings.HasSuffix(p, ".dll"))))
		}
		if group == "rust" {
			if isDir {
				return p == "bin" || p == "lib" || strings.HasPrefix(p, "lib/rustlib")
			}
			return strings.HasPrefix(p, "bin/") || strings.HasPrefix(p, "lib/rustlib/")
		}
		if group == "pwsh" || group == "pnpm" || group == "yarn" {
			if isDir {
				return !strings.HasPrefix(p, ".")
			}
			return true
		}
		if isDir {
			return (group == "python" && (p == "lib" || strings.HasPrefix(p, "lib/") || p == "dlls" || strings.HasPrefix(p, "dlls/"))) ||
				(group == "go" && (p == "bin" || p == "src" || p == "pkg" || p == "lib" || strings.HasPrefix(p, "src/") || strings.HasPrefix(p, "pkg/") || strings.HasPrefix(p, "lib/")))
		}
		if p == strings.ToLower(filepath.ToSlash(relative)) {
			return true
		}
		switch group {
		case "git", "node":
			return !strings.Contains(p, "/") && strings.HasSuffix(p, ".dll")
		case "python":
			return (!strings.Contains(p, "/") && strings.HasSuffix(p, ".dll")) || strings.HasPrefix(p, "lib/") || strings.HasPrefix(p, "dlls/")
		case "go":
			return strings.HasPrefix(p, "src/") || strings.HasPrefix(p, "pkg/") || strings.HasPrefix(p, "lib/") || p == "version" || p == "go.env"
		}
		return false
	}
	var size int64
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, e error) error {
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
			return os.MkdirAll(dest, 0700)
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
		if size > 3<<30 {
			return fmt.Errorf("工具链超过 3 GiB")
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
}

func toolchainEnv(group, target string) []string {
	switch group {
	case "npm":
		return []string{"NODE_OPTIONS=--preserve-symlinks --preserve-symlinks-main", "NPM_CONFIG_OFFLINE=true", "NPM_CONFIG_UPDATE_NOTIFIER=false", "NPM_CONFIG_AUDIT=false", "NPM_CONFIG_FUND=false"}
	case "go":
		return []string{"GOROOT=" + target, "GOTELEMETRY=off"}
	case "python":
		return []string{"PYTHONNOUSERSITE=1", "PYTHONHOME=" + target}
	case "rust":
		return []string{"CARGO_NET_OFFLINE=true"}
	}
	return nil
}

// Copy only the executable distribution into a versioned read-only cache (or per-run
// fallback if named capabilities are unavailable), never exposing host ACLs or user caches.
func privateToolchain(ctx context.Context, name, exe, dir, cacheDir string, roSID *windows.SID) (string, []string, bool, error) {
	if name == browserRenderer {
		target, env, err := stageHTMLRenderer(ctx, exe, dir)
		return target, env, false, err
	}
	if name == "cmd" || name == "powershell" {
		return exe, nil, false, nil
	}
	group := toolchainGroup(name)
	root, relative, err := toolchainRootAndRel(ctx, group, exe)
	if err != nil {
		return "", nil, false, err
	}
	if roSID != nil {
		info, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if statErr == nil {
			keyInput := fmt.Sprintf("%s|%s|%s|%d|%d", toolchainCacheSchema, group, strings.ToLower(filepath.Clean(root)), info.Size(), info.ModTime().UnixNano())
			if group == "python" {
				if sp, e := os.Stat(filepath.Join(root, "Lib", "site-packages")); e == nil {
					keyInput += fmt.Sprintf("|sp:%d", sp.ModTime().UnixNano())
				}
			}
			fp := fmt.Sprintf("%x", sha256.Sum256([]byte(keyInput)))[:24]
			cacheBase := resolveCacheBase(cacheDir)
			finalDir := filepath.Join(cacheBase, group+"-"+fp)
			readyMarker := filepath.Join(finalDir, ".ready")
			if _, e := os.Stat(readyMarker); e == nil {
				return filepath.Join(finalDir, filepath.FromSlash(relative)), toolchainEnv(group, finalDir), true, nil
			}
			toolchainCacheMu.Lock()
			defer toolchainCacheMu.Unlock()
			if _, e := os.Stat(readyMarker); e == nil {
				return filepath.Join(finalDir, filepath.FromSlash(relative)), toolchainEnv(group, finalDir), true, nil
			}
			if e := os.MkdirAll(cacheBase, 0700); e != nil {
				return "", nil, false, e
			}
			building := filepath.Join(cacheBase, ".build-"+group+"-"+randomHex(6))
			if e := os.Mkdir(building, 0700); e != nil {
				return "", nil, false, e
			}
			// Let the read-only ACE inherit as files are created. Applying it
			// afterwards requires Windows to revisit the entire distribution.
			if e := privateACL(building, roSID, 0x1200a9); e != nil {
				_ = os.RemoveAll(building)
				return "", nil, false, fmt.Errorf("工具链缓存 ACL: %w", e)
			}
			if e := copyToolchainTree(ctx, group, root, relative, building); e != nil {
				_ = os.RemoveAll(building)
				return "", nil, false, fmt.Errorf("复制 %s 工具链: %w", group, e)
			}
			if e := os.WriteFile(filepath.Join(building, ".ready"), []byte(keyInput), 0600); e != nil {
				_ = os.RemoveAll(building)
				return "", nil, false, e
			}
			if e := os.Rename(building, finalDir); e != nil {
				_ = os.RemoveAll(building)
				if _, readyErr := os.Stat(readyMarker); readyErr != nil {
					return "", nil, false, e
				}
			}
			return filepath.Join(finalDir, filepath.FromSlash(relative)), toolchainEnv(group, finalDir), true, nil
		}
	}
	target := filepath.Join(dir, "tools")
	if e := os.Mkdir(target, 0700); e != nil {
		return "", nil, false, e
	}
	if e := copyToolchainTree(ctx, group, root, relative, target); e != nil {
		return "", nil, false, e
	}
	return filepath.Join(target, filepath.FromSlash(relative)), toolchainEnv(group, target), false, nil
}

// Incrementally sync host GOMODCACHE into a read-only cache readable by roSID so
// real Go projects with third-party modules can run offline `go test` inside LPAC.
func hasGoMod(work, cwd string) bool {
	cur := filepath.Clean(filepath.Join(work, filepath.FromSlash(cwd)))
	root := filepath.Clean(work)
	for {
		if st, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil && st.Mode().IsRegular() {
			return true
		}
		if strings.EqualFold(cur, root) {
			return false
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
}

func cachedGoModCache(ctx context.Context, hostGoExe, cacheDir string, roSID *windows.SID) (string, error) {
	if roSID == nil {
		return "", nil
	}
	out, err := exec.CommandContext(ctx, hostGoExe, "env", "GOMODCACHE").Output()
	if err != nil {
		return "", nil
	}
	src := filepath.Clean(strings.TrimSpace(string(out)))
	if src == "" || src == "." {
		return "", nil
	}
	info, err := os.Lstat(src)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", nil
	}
	toolchainCacheMu.Lock()
	defer toolchainCacheMu.Unlock()
	destRoot := filepath.Join(resolveCacheBase(cacheDir), "gomodcache")
	if err := os.MkdirAll(destRoot, 0700); err != nil {
		return "", err
	}
	var copiedBytes int64
	var files int
	err = filepath.WalkDir(src, func(path string, entry fs.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rel, e := filepath.Rel(src, path)
		if e != nil || rel == "." {
			return nil
		}
		if isSensitiveName(filepath.ToSlash(rel)) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		dest := filepath.Join(destRoot, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		files++
		if files > 180000 {
			return fs.SkipAll
		}
		if st, statErr := os.Lstat(dest); statErr == nil && st.Mode().IsRegular() {
			copiedBytes += st.Size()
			return nil
		}
		srcInfo, statErr := entry.Info()
		if statErr != nil {
			return nil
		}
		copiedBytes += srcInfo.Size()
		if copiedBytes > 2<<30 {
			return fs.SkipAll
		}
		in, openErr := os.Open(path)
		if openErr != nil {
			return nil
		}
		defer in.Close()
		tmp := dest + ".tmp-" + randomHex(4)
		outFile, createErr := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr != nil {
			return nil
		}
		_, cpErr := io.Copy(outFile, in)
		closeErr := outFile.Close()
		if cpErr != nil || closeErr != nil {
			_ = os.Remove(tmp)
			return nil
		}
		if renErr := os.Rename(tmp, dest); renErr != nil {
			_ = os.Remove(tmp)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if err := privateACL(destRoot, roSID, 0x1200a9); err != nil {
		return "", err
	}
	return destRoot, nil
}

func nodeModulesFingerprint(projectDir, nmDir string) string {
	var b strings.Builder
	b.WriteString(toolchainCacheSchema)
	b.WriteByte('|')
	b.WriteString(strings.ToLower(filepath.Clean(nmDir)))
	foundLock := false
	for _, name := range []string{"package-lock.json", "pnpm-lock.yaml", "yarn.lock", "package.json"} {
		if info, err := os.Stat(filepath.Join(projectDir, name)); err == nil && info.Mode().IsRegular() {
			fmt.Fprintf(&b, "|%s:%d:%d", name, info.Size(), info.ModTime().UnixNano())
			foundLock = true
		}
	}
	if !foundLock {
		if entries, err := os.ReadDir(nmDir); err == nil {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			sort.Strings(names)
			if len(names) > 64 {
				names = names[:64]
			}
			b.WriteString("|top:")
			b.WriteString(strings.Join(names, ","))
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(b.String())))[:24]
}

// Mirror a project's node_modules into a read-only cache and expose it via a
// temporary NTFS directory junction inside work for the duration of one command.
// Every path component in both projectRoot and work is checked for symlink escapes,
// and junctions are created via Win32 FSCTL_SET_REPARSE_POINT (no shell / cmd.exe).
func mountReadOnlyNodeModules(ctx context.Context, projectRoot, work, cwd, cacheDir string, roSID *windows.SID) ([]string, func()) {
	if projectRoot == "" || roSID == nil {
		return nil, func() {}
	}
	candidates := []string{"."}
	if cwd != "" && cwd != "." {
		candidates = append([]string{filepath.ToSlash(cwd)}, candidates...)
	}
	var junctions []string
	var binPaths []string
	cleanup := func() {
		for _, j := range junctions {
			_ = os.Remove(j)
		}
	}
	for _, relDir := range candidates {
		relNM := "node_modules"
		if relDir != "." {
			relNM = relDir + "/node_modules"
		}
		srcNM, ok := safeContainedRealDir(projectRoot, relNM)
		if !ok {
			continue
		}
		srcProjectDir := filepath.Dir(srcNM)
		dstNM, ok := safeContainedChildPath(work, relDir, "node_modules")
		if !ok {
			continue
		}
		cached, err := ensureCachedNodeModules(ctx, srcProjectDir, srcNM, cacheDir, roSID)
		if err != nil || cached == "" {
			continue
		}
		if err := createDirectoryJunction(dstNM, cached); err != nil {
			continue
		}
		junctions = append(junctions, dstNM)
		binDir := filepath.Join(dstNM, ".bin")
		if binSt, err := os.Stat(binDir); err == nil && binSt.IsDir() {
			binPaths = append(binPaths, binDir)
		}
	}
	return binPaths, cleanup
}

func ensureCachedNodeModules(ctx context.Context, srcProjectDir, srcNM, cacheDir string, roSID *windows.SID) (string, error) {
	fp := nodeModulesFingerprint(srcProjectDir, srcNM)
	cacheBase := resolveCacheBase(cacheDir)
	finalDir := filepath.Join(cacheBase, "nm-"+fp)
	readyMarker := filepath.Join(finalDir, ".ready")
	if _, e := os.Stat(readyMarker); e == nil {
		return finalDir, nil
	}
	toolchainCacheMu.Lock()
	defer toolchainCacheMu.Unlock()
	if _, e := os.Stat(readyMarker); e == nil {
		return finalDir, nil
	}
	if e := os.MkdirAll(cacheBase, 0700); e != nil {
		return "", e
	}
	building := filepath.Join(cacheBase, ".build-nm-"+randomHex(6))
	if e := os.Mkdir(building, 0700); e != nil {
		return "", e
	}
	var totalBytes int64
	var totalFiles int
	walkErr := filepath.WalkDir(srcNM, func(path string, entry fs.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rel, e := filepath.Rel(srcNM, path)
		if e != nil || rel == "." {
			return nil
		}
		slash := filepath.ToSlash(rel)
		if entry.IsDir() {
			base := strings.ToLower(entry.Name())
			if base == ".cache" || base == ".git" {
				return fs.SkipDir
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fs.SkipDir
			}
			return os.MkdirAll(filepath.Join(building, rel), 0700)
		}
		if isSensitiveName(slash) || !entry.Type().IsRegular() {
			return nil
		}
		totalFiles++
		if totalFiles > 120000 {
			return fmt.Errorf("node_modules 文件数超过缓存上限")
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		totalBytes += info.Size()
		if totalBytes > 1<<30 {
			return fmt.Errorf("node_modules 体积超过 1 GiB 缓存上限")
		}
		in, openErr := os.Open(path)
		if openErr != nil {
			return nil
		}
		defer in.Close()
		out, createErr := os.OpenFile(filepath.Join(building, rel), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr != nil {
			return nil
		}
		_, cpErr := io.Copy(out, in)
		closeErr := out.Close()
		if cpErr != nil {
			return cpErr
		}
		return closeErr
	})
	if walkErr != nil {
		_ = os.RemoveAll(building)
		return "", walkErr
	}
	if e := privateACL(building, roSID, 0x1200a9); e != nil {
		_ = os.RemoveAll(building)
		return "", e
	}
	if e := os.WriteFile(filepath.Join(building, ".ready"), []byte(fp), 0600); e != nil {
		_ = os.RemoveAll(building)
		return "", e
	}
	if e := os.Rename(building, finalDir); e != nil {
		_ = os.RemoveAll(building)
		if _, readyErr := os.Stat(readyMarker); readyErr != nil {
			return "", e
		}
	}
	return finalDir, nil
}
