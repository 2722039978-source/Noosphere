package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/noosphere/backend/internal/ownedprocess"
)

var testArguments = map[string][]string{
	"go":     {"test", "-timeout", "60s", "./..."},
	"python": {"-I", "-X", "utf8", "-m", "unittest", "discover", "-v"},
	"node":   {"--test"},
}

type testInvocation struct {
	Executable  string `json:"executable"`
	SHA256      string `json:"sha256"`
	RootID      string `json:"root_id"`
	DirectoryID string `json:"directory_id"`
}

func planTest(a *Action) error {
	args := testArguments[a.Runner]
	if len(args) == 0 {
		return fmt.Errorf("未知测试运行器")
	}
	directory, rootID, directoryID, err := testDirectory(*a)
	if err != nil {
		return err
	}
	directory.Close()
	executable, err := discoverTestExecutable(a.Root, a.Runner)
	if err != nil {
		return err
	}
	digest, err := executableDigest(executable)
	if err != nil {
		return err
	}
	a.Test = &testInvocation{Executable: executable, SHA256: digest, RootID: rootID, DirectoryID: directoryID}
	a.Command = fmt.Sprintf("%q %s", executable, strings.Join(args, " "))
	return nil
}

func testDirectory(a Action) (*os.File, string, string, error) {
	root, err := openProjectRoot(a.Root)
	if err != nil {
		return nil, "", "", err
	}
	defer root.Close()
	rootFile, err := root.Open(".")
	if err != nil {
		return nil, "", "", err
	}
	defer rootFile.Close()
	rootID, err := directoryIdentity(rootFile)
	if err != nil {
		return nil, "", "", err
	}
	directory, err := checkedDirectory(root, a.Path)
	if err != nil {
		return nil, "", "", err
	}
	dirID, err := directoryIdentity(directory)
	if err != nil {
		directory.Close()
		return nil, "", "", err
	}
	return directory, rootID, dirID, nil
}

func discoverTestExecutable(project, runner string) (string, error) {
	key := "NOOSPHERE_TEST_" + strings.ToUpper(runner)
	failure := fmt.Errorf("未找到可信的 %s 项目测试工具链。请安装，或将 %s 指向项目之外的绝对可执行文件路径", runner, key)
	if explicit, set := os.LookupEnv(key); set {
		if path := trustedTestExecutable(project, explicit); path != "" {
			return path, nil
		}
		return "", failure
	}

	names := []string{runner}
	if runner == "python" && runtime.GOOS != "windows" {
		names = []string{"python3", "python"}
	}
	for _, name := range names {
		if candidate, err := exec.LookPath(name); err == nil {
			if path := trustedTestExecutable(project, candidate); path != "" {
				return path, nil
			}
		}
	}
	if runtime.GOOS != "windows" {
		dirs := []string{"/usr/local/bin", "/usr/bin", "/bin"}
		if runtime.GOOS == "darwin" {
			dirs = append([]string{"/opt/homebrew/bin"}, dirs...)
		}
		if runner == "go" {
			dirs = append(dirs, "/usr/local/go/bin")
		}
		for _, dir := range dirs {
			for _, name := range names {
				if path := trustedTestExecutable(project, filepath.Join(dir, name)); path != "" {
					return path, nil
				}
			}
		}
	}
	return "", failure
}

func trustedTestExecutable(project, candidate string) string {
	if !filepath.IsAbs(candidate) {
		return ""
	}
	candidate = filepath.Clean(candidate)
	path, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(path), ".exe") {
		return ""
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
		return ""
	}
	for _, key := range []string{"NOOSPHERE_CODELENS_PYTHON", "NOOSPHERE_REA_NODE"} {
		if private, err := os.Stat(os.Getenv(key)); err == nil && os.SameFile(info, private) {
			return ""
		}
	}
	root, err := os.Stat(project)
	if err != nil {
		return ""
	}
	for _, name := range []string{candidate, path} {
		for dir := filepath.Dir(name); ; dir = filepath.Dir(dir) {
			ancestor, err := os.Stat(dir)
			if err != nil || os.SameFile(root, ancestor) {
				return ""
			}
			if dir == filepath.Dir(dir) {
				break
			}
		}
	}
	// Preserve the frontend path of a user-configured external virtualenv.
	// Invoking a resolved base interpreter can discard pyvenv.cfg.
	return candidate
}

func executableDigest(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 512<<20 {
		return "", fmt.Errorf("测试工具链必须是 512 MiB 以内的普通可执行文件")
	}
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(file, (512<<20)+1))
	if err != nil || n != info.Size() {
		return "", fmt.Errorf("测试工具链读取失败或已变化")
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func executeTest(ctx context.Context, a Action) (string, error) {
	args := testArguments[a.Runner]
	if len(args) == 0 || a.Test == nil {
		return "", fmt.Errorf("测试预览缺少已核实的工具链，请重新提出测试")
	}
	directory, rootID, dirID, err := testDirectory(a)
	if err != nil {
		return "", err
	}
	defer directory.Close()
	if rootID != a.Test.RootID || dirID != a.Test.DirectoryID {
		return "", fmt.Errorf("测试目录已替换，请重新提出测试")
	}
	executable := trustedTestExecutable(a.Root, a.Test.Executable)
	digest, err := executableDigest(executable)
	if executable != a.Test.Executable || err != nil || digest != a.Test.SHA256 {
		return "", fmt.Errorf("测试工具链已变化，请重新提出测试")
	}
	cmd := exec.Command(executable, args...)
	cmd.Dir = filepath.Join(a.Root, filepath.FromSlash(a.Path))
	cmd.Env = append(ownedprocess.BaseEnv(), "PATH="+testSearchPath(executable),
		"GOTOOLCHAIN=local", "GOENV=off")
	return executeCommand(ctx, cmd, directory)
}

func testSearchPath(executable string) string {
	dirs := []string{filepath.Dir(executable)}
	if runtime.GOOS == "windows" {
		if systemRoot := os.Getenv("SystemRoot"); filepath.IsAbs(systemRoot) {
			dirs = append(dirs, filepath.Join(systemRoot, "System32"), systemRoot)
		}
	} else {
		dirs = append(dirs, "/usr/bin", "/bin")
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}
