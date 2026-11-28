package sandbox

import (
	"context"
	"crypto/sha256"

	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var createProfile = windows.NewLazySystemDLL("userenv.dll").NewProc("CreateAppContainerProfile")
var deleteProfile = windows.NewLazySystemDLL("userenv.dll").NewProc("DeleteAppContainerProfile")
var deriveSID = windows.NewLazySystemDLL("userenv.dll").NewProc("DeriveAppContainerSidFromAppContainerName")

type securityCapabilities struct {
	SID             *windows.SID
	Caps            *windows.SIDAndAttributes
	Count, Reserved uint32
}

func privateACL(path string, sid *windows.SID, access windows.ACCESS_MASK) error {
	sd, e := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		return e
	}
	old, _, e := sd.DACL()
	if e != nil {
		return e
	}
	// Modify grants ordinary file access, never ownership or write-DACL.
	acl, e := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{AccessPermissions: access, AccessMode: windows.SET_ACCESS, Inheritance: 3, Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeValue: windows.TrusteeValueFromSID(sid)}}}, old)
	if e != nil {
		return e
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
func windowsTool(name, system string) (string, error) {
	switch name {
	case "cmd":
		return filepath.Join(system, "cmd.exe"), nil
	case "powershell":
		return filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), nil
	case "pwsh", "python", "python3", "pytest", "node", "go", "git", "npm", "npx", "pnpm", "yarn", "cargo", "rustc":
		lookup := []string{name}
		switch name {
		case "npm", "npx":
			lookup = []string{"node"}
		case "pytest":
			lookup = []string{"python", "python3"}
		case "python3":
			lookup = []string{"python3", "python"}
		}
		for _, cand := range lookup {
			p, e := exec.LookPath(cand + ".exe")
			if e == nil && filepath.IsAbs(p) {
				resolved, err := filepath.EvalSymlinks(p)
				if err != nil {
					return "", fmt.Errorf("解析 %s 工具链位置: %w", name, err)
				}
				return resolved, nil
			}
		}
	}
	return "", fmt.Errorf("程序未登记或未安装：%s", name)
}
func resolveProgram(program, projectRoot string) (string, error) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return "", err
	}
	return resolveWindowsProgram(program, projectRoot, system)
}

func resolveWindowsProgram(program, projectRoot, system string) (string, error) {
	exe, err := windowsTool(program, system)
	if err != nil {
		return "", err
	}
	if err := checkToolLocation(exe, projectRoot); err != nil {
		return "", err
	}
	return exe, nil
}

func launch(ctx context.Context, work, cwd, program string, args []string, out io.Writer, limits Limits) (int, error) {
	system, e := windows.GetSystemDirectory()
	if e != nil {
		return -1, e
	}
	exe, e := resolveWindowsProgram(program, limits.ProjectRoot, system)
	if e != nil {
		return -1, e
	}
	return launchResolved(ctx, work, cwd, program, exe, system, args, out, limits)
}
func launchResolved(ctx context.Context, work, cwd, program, exe, system string, args []string, out io.Writer, limits Limits) (int, error) {
	origExe := exe
	// Keep HOME and TEMP shallow even when a project or test harness places
	// the draft under deeply nested state directories.
	scratchRoot, e := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if e != nil {
		return -1, fmt.Errorf("定位 Windows 私有临时目录: %w", e)
	}
	runtimeDir, e := os.MkdirTemp(scratchRoot, ".noosphere-command-")
	if e != nil {
		return -1, e
	}
	defer os.RemoveAll(runtimeDir)
	name, _ := windows.UTF16PtrFromString(fmt.Sprintf("Noosphere.Agent.%x", sha256.Sum256([]byte(strings.ToLower(work))))[:48])
	var sid *windows.SID
	hr, _, _ := createProfile.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), 0, 0, uintptr(unsafe.Pointer(&sid)))
	if uint32(hr) == 0x800700b7 {
		hr, _, _ = deriveSID.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&sid)))
	}
	if hr != 0 || sid == nil {
		return -1, fmt.Errorf("AppContainer SID: HRESULT %#x", hr)
	}
	defer windows.FreeSid(sid)
	defer deleteProfile.Call(uintptr(unsafe.Pointer(name)))
	privateHome := filepath.Join(runtimeDir, "home")
	if e = os.Mkdir(privateHome, 0700); e != nil {
		return -1, e
	}
	scratchBytes := int64(2 << 30)
	if limits.DiskBytes > 0 {
		scratchBytes = limits.DiskBytes
	}
	ctx, cancelScratch := context.WithCancelCause(ctx)
	scratchDone := monitorDisk(ctx, cancelScratch, privateHome, scratchBytes)
	defer func() { cancelScratch(nil); <-scratchDone }()
	if e = privateACL(runtimeDir, sid, 0x1200a9); e != nil {
		return -1, fmt.Errorf("命令运行目录 ACL: %w", e)
	}
	roSID, _ := readonlyCapability()
	exe, toolEnv, _, e := privateToolchain(ctx, program, exe, runtimeDir, limits.CacheDir, roSID)
	if e != nil {
		return -1, fmt.Errorf("准备 %s 工具链: %w", program, e)
	}
	switch program {
	case "npm":
		args = append([]string{filepath.Join(filepath.Dir(exe), "node_modules", "npm", "bin", "npm-cli.js")}, args...)
	case "npx":
		args = append([]string{filepath.Join(filepath.Dir(exe), "node_modules", "npm", "bin", "npx-cli.js")}, args...)
	case "pytest":
		args = append([]string{"-m", "pytest"}, args...)
	}
	if program == "go" && hasGoMod(work, cwd) {
		if modCache, modErr := cachedGoModCache(ctx, origExe, limits.CacheDir, roSID); modErr == nil && modCache != "" {
			toolEnv = append(toolEnv, "GOMODCACHE="+modCache, "GOCACHE="+filepath.Join(privateHome, "go-build"))
		}
	}
	for _, dir := range []string{work, privateHome} {
		if e = privateACL(dir, sid, 0x1301bf); e != nil {
			return -1, fmt.Errorf("private workspace ACL: %w", e)
		}
	}
	// Mount read-only node_modules junctions strictly AFTER setting writable ACL on work,
	// so NTFS ACL inheritance never touches the shared read-only cache.
	var extraPaths []string
	if program == "node" || program == "npm" || program == "npx" || program == "pnpm" || program == "yarn" {
		binDirs, cleanupNM := mountReadOnlyNodeModules(ctx, limits.ProjectRoot, work, cwd, limits.CacheDir, roSID)
		defer cleanupNM()
		extraPaths = append(extraPaths, binDirs...)
	}
	job, e := windows.CreateJobObject(nil, nil)
	if e != nil {
		return -1, e
	}
	defer windows.CloseHandle(job)
	jobLimits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	jobLimits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_JOB_MEMORY
	jobLimits.BasicLimitInformation.ActiveProcessLimit = 64
	jobLimits.JobMemoryLimit = 2 << 30
	if _, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&jobLimits)), uint32(unsafe.Sizeof(jobLimits))); e != nil {
		return -1, e
	}
	reader, writer, e := os.Pipe()
	if e != nil {
		return -1, e
	}
	defer reader.Close()
	defer writer.Close()
	input, e := os.Open(os.DevNull)
	if e != nil {
		return -1, e
	}
	defer input.Close()
	handles := []windows.Handle{windows.Handle(input.Fd()), windows.Handle(writer.Fd())}
	for _, h := range handles {
		if e = windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); e != nil {
			return -1, e
		}
	}
	registrySID, e := registryCapability()
	if e != nil {
		return -1, e
	}
	capList := []windows.SIDAndAttributes{{Sid: registrySID, Attributes: windows.SE_GROUP_ENABLED}}
	if roSID != nil {
		capList = append(capList, windows.SIDAndAttributes{Sid: roSID, Attributes: windows.SE_GROUP_ENABLED})
	}
	caps := securityCapabilities{SID: sid, Caps: &capList[0], Count: uint32(len(capList))}
	optOutAllAppPackages := uint32(1)
	attrs, e := windows.NewProcThreadAttributeList(4)
	if e != nil {
		return -1, e
	}
	defer attrs.Delete()
	// Assign the Job and LPAC restrictions at process creation, before any child code runs.
	for _, a := range []struct {
		key  uintptr
		ptr  unsafe.Pointer
		size uintptr
	}{
		{0x20009, unsafe.Pointer(&caps), unsafe.Sizeof(caps)}, {0x2000f, unsafe.Pointer(&optOutAllAppPackages), 4},
		{0x2000d, unsafe.Pointer(&job), unsafe.Sizeof(job)}, {windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), 2 * unsafe.Sizeof(job)},
	} {
		if e = attrs.Update(a.key, a.ptr, a.size); e != nil {
			return -1, fmt.Errorf("process attribute %x: %w", a.key, e)
		}
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = windows.STARTF_USESTDHANDLES
	si.StdInput = handles[0]
	si.StdOutput = handles[1]
	si.StdErr = handles[1]
	var pi windows.ProcessInformation
	pathParts := append(extraPaths, filepath.Dir(exe), system)
	env := []string{"APPDATA=" + privateHome, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GOENV=off", "GOPROXY=off", "GOTOOLCHAIN=local", "HOME=" + privateHome, "LOCALAPPDATA=" + privateHome, "PATH=" + strings.Join(pathParts, ";"), "SystemRoot=" + filepath.Dir(system), "TEMP=" + privateHome, "TMP=" + privateHome, "USERPROFILE=" + privateHome, "WINDIR=" + filepath.Dir(system)}
	env = append(env, toolEnv...)
	sort.Strings(env)
	block := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	app, _ := windows.UTF16PtrFromString(exe)
	dir, _ := windows.UTF16PtrFromString(filepath.Join(work, cwd))
	line, e := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{exe}, args...)))
	if e != nil {
		return -1, e
	}
	if e = ctx.Err(); e != nil {
		return -1, e
	}
	e = windows.CreateProcess(app, line, nil, nil, true, windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NO_WINDOW, &block[0], dir, &si.StartupInfo, &pi)
	runtime.KeepAlive(caps)
	runtime.KeepAlive(capList)
	runtime.KeepAlive(registrySID)
	runtime.KeepAlive(roSID)
	runtime.KeepAlive(handles)
	runtime.KeepAlive(optOutAllAppPackages)
	if e != nil {
		return -1, fmt.Errorf("CreateProcess %s: %w", exe, e)
	}
	defer windows.CloseHandle(pi.Process)
	windows.CloseHandle(pi.Thread)
	writer.Close()
	drained := make(chan error, 1)
	go func() { _, err := io.Copy(out, reader); drained <- err }()
	killed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { windows.TerminateJobObject(job, 1); close(killed) })
	_, e = windows.WaitForSingleObject(pi.Process, windows.INFINITE)
	if !stop() {
		<-killed
	}
	// A descendant may still own the output pipe or keep writing to the draft.
	if end := windows.TerminateJobObject(job, 1); end != nil && e == nil {
		e = end
	}
	if e == nil {
		e = waitForJobExit(job)
	}
	if e != nil {
		_ = reader.Close()
	}
	if err := <-drained; e == nil {
		e = err
	}
	if e != nil {
		return -1, e
	}
	if ctx.Err() != nil {
		return -1, context.Cause(ctx)
	}
	if e := scopedDiskUsage(ctx, privateHome, scratchBytes); e != nil {
		return -1, e
	}
	var code uint32
	if e = windows.GetExitCodeProcess(pi.Process, &code); e != nil {
		return -1, e
	}
	return int(code), nil
}
