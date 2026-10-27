package evalrunner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This native process fixture tests only compiler containment. Model code never
// reaches this helper; actual model execution tests use Wasm exclusively.
func TestCompilerJobHelper(t *testing.T) {
	role := os.Getenv("EVALRUNNER_JOB_HELPER")
	if role == "" {
		return
	}
	if role == "memory" {
		allocation, err := windows.VirtualAlloc(0, CompilerMemoryLimitBytes+65536, windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
		if err == nil {
			windows.VirtualFree(allocation, 0, windows.MEM_RELEASE)
			os.Exit(3)
		}
		os.Exit(0)
	}
	if role == "root" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCompilerJobHelper$")
		cmd.Env = []string{"SystemRoot=" + os.Getenv("SystemRoot"), "EVALRUNNER_JOB_HELPER=leaf", "EVALRUNNER_PID_FILE=" + os.Getenv("EVALRUNNER_PID_FILE")}
		if err := cmd.Start(); err != nil {
			os.Exit(4)
		}
	} else if role == "leaf" {
		if err := os.WriteFile(os.Getenv("EVALRUNNER_PID_FILE"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			os.Exit(5)
		}
	} else {
		os.Exit(6)
	}
	time.Sleep(30 * time.Second)
	os.Exit(7)
}

func TestCompilerProcessTreeContainedAndReleased(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCompilerJobHelper$")
	cmd.Env = []string{"SystemRoot=" + os.Getenv("SystemRoot"), "EVALRUNNER_JOB_HELPER=root", "EVALRUNNER_PID_FILE=" + pidFile}
	process, err := prepareCompiler(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer process.close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Before contain/resume the root cannot have started any descendant.
	time.Sleep(30 * time.Millisecond)
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal("process executed before containment")
	}
	if err := process.contain(cmd); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill() }()
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if err := windows.QueryInformationJobObject(process.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
		t.Fatal(err)
	}
	if info.JobMemoryLimit != CompilerMemoryLimitBytes || info.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_JOB_MEMORY == 0 {
		t.Fatalf("compiler Job memory limit absent: %+v", info)
	}
	var pid int
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			continue
		}
		pid, err = strconv.Atoi(string(data))
		if err == nil {
			break
		}
	}
	if pid == 0 {
		cancel()
		cmd.Wait()
		t.Fatal("descendant did not start")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		cancel()
		cmd.Wait()
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	var accounting struct {
		TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
		TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
	}
	if err := windows.QueryInformationJobObject(process.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil); err != nil {
		t.Fatal(err)
	}
	if accounting.ActiveProcesses != 2 {
		t.Fatalf("expected root+descendant in compiler Job: %+v", accounting)
	}
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Fatal("cancelled process succeeded")
	}
	status, err := windows.WaitForSingleObject(handle, 5000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant survived cancellation: %d %v", status, err)
	}
	t.Logf("suspended-before-join proven; memory limit %d; root+leaf joined; both terminated", info.JobMemoryLimit)
}

func TestCompilerJobDeniesOverBudgetCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCompilerJobHelper$")
	cmd.Env = []string{"SystemRoot=" + os.Getenv("SystemRoot"), "EVALRUNNER_JOB_HELPER=memory"}
	process, err := prepareCompiler(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer process.close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := process.contain(cmd); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("memory probe failed: %v", err)
	}
	t.Log("actual allocation above 2GiB denied inside compiler Job")
}

func TestCloseDuringCompileReleasesOwnedDirectory(t *testing.T) {
	r := newTestRunner(t)
	result := make(chan error, 1)
	go func() {
		_, err := r.Compile(context.Background(), CompileSpec{Source: "package main;func Solve(x int)int{return x}", FunctionName: "Solve", InputType: "int", OutputType: "int"})
		result <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(r.compileGate) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(r.compileGate) == 0 {
		t.Fatal("compiler never admitted")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("closed runner compiler must preserve cancellation: %v", err)
	}
	if _, err := os.Stat(r.root); !os.IsNotExist(err) {
		t.Fatal(fmt.Errorf("owned compile directory remains: %v", err))
	}
}
