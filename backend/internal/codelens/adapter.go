package codelens

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Adapter struct {
	cmdArgs        []string
	port           int
	cmd            *exec.Cmd
	proxy          *httputil.ReverseProxy
	done           chan struct{}
	waitErr        error // written before done closes
	ModifyResponse func(*http.Response) error
	TraceDirectory string
	token          string
}

// NewAdapter creates a CodeLens adapter. cmdArgs should be the Python
// executable and arguments, e.g. ["python"] or ["py", "-3"].
func NewAdapter(cmdArgs []string, scriptPath string, port int) *Adapter {
	return &Adapter{
		cmdArgs: append(cmdArgs, scriptPath),
		port:    port,
		token:   rand.Text(),
	}
}

func (a *Adapter) Start() (err error) {
	return a.StartContext(context.Background())
}

func (a *Adapter) StartContext(parent context.Context) (err error) {
	if len(a.cmdArgs) < 2 || a.cmdArgs[0] == "" {
		return fmt.Errorf("codelens Python command is empty")
	}
	if a.port != 0 {
		probe, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", a.port))
		if err != nil {
			return fmt.Errorf("codelens port %d is occupied: %w", a.port, err)
		}
		probe.Close()
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	a.cmd = exec.Command(a.cmdArgs[0], a.cmdArgs[1:]...)
	a.cmd.WaitDelay = 2 * time.Second
	a.cmd.Env = append(os.Environ(), fmt.Sprintf("NOOSPHERE_CODELENS_PORT=%d", a.port), "NOOSPHERE_CODELENS_TOKEN="+a.token)
	// The Python service needs only its own transport credential.
	a.cmd.Env = removeEnv(a.cmd.Env, "NOOSPHERE_BACKEND_TOKEN")
	a.cmd.Env = append(a.cmd.Env, "NOOSPHERE_CODELENS_TRACE_DIR="+a.TraceDirectory)
	a.cmd.Stderr = log.Writer()
	var stdout io.ReadCloser
	if a.port == 0 {
		stdout, err = a.cmd.StdoutPipe()
		if err != nil {
			return err
		}
	} else {
		a.cmd.Stdout = log.Writer()
	}
	configureWorker(a.cmd)
	log.Printf("[codelens] starting Python worker: %v", a.cmdArgs)
	if err := a.cmd.Start(); err != nil {
		if stdout != nil {
			_ = stdout.Close()
		}
		return fmt.Errorf("start codelens: %w", err)
	}
	a.done = make(chan struct{})
	reported := make(chan int, 1)
	drained := make(chan struct{})
	if stdout == nil {
		close(drained)
	} else {
		go readWorkerOutput(stdout, reported, drained)
	}
	go func() {
		<-drained
		a.waitErr = a.cmd.Wait()
		close(a.done)
	}()
	defer func() {
		if err != nil {
			a.Stop()
		}
	}()
	if a.port == 0 {
		select {
		case a.port = <-reported:
		case <-a.done:
			return fmt.Errorf("codelens Python worker exited before ready: %v", a.waitErr)
		case <-ctx.Done():
			return fmt.Errorf("waiting for CodeLens listener: %w", ctx.Err())
		}
	}
	client := newHealthClient()
	addr := fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", a.port)
	for ctx.Err() == nil {
		select {
		case <-a.done:
			return fmt.Errorf("codelens Python worker exited before ready: %v", a.waitErr)
		default:
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", addr, nil)
		req.Header.Set("Authorization", "Bearer "+a.token)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		if err == nil && resp.StatusCode == http.StatusOK {
			target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", a.port))
			a.proxy = &httputil.ReverseProxy{
				Transport:      &http.Transport{DisableKeepAlives: true},
				ModifyResponse: a.ModifyResponse,
				Rewrite: func(pr *httputil.ProxyRequest) {
					pr.Out.URL.Scheme = target.Scheme
					pr.Out.URL.Host = target.Host
					pr.Out.URL.Path = strings.Replace(pr.In.URL.Path, "/api/v1/codelens", "/api/v1", 1)
					pr.Out.Host = target.Host
					pr.Out.Header.Set("Authorization", "Bearer "+a.token)
				},
			}
			log.Printf("[codelens] worker ready on :%d", a.port)
			return nil
		}
		select {
		case <-a.done:
			return fmt.Errorf("codelens Python worker exited before ready: %v", a.waitErr)
		case <-ctx.Done():
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("codelens worker not ready: %w", ctx.Err())
}

func removeEnv(entries []string, name string) []string {
	result := entries[:0]
	for _, entry := range entries {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, name) {
			result = append(result, entry)
		}
	}
	return result
}

func readWorkerOutput(stdout io.ReadCloser, reported chan<- int, drained chan<- struct{}) {
	defer close(drained)
	defer stdout.Close()
	scanner := bufio.NewScanner(stdout)
	sent := false
	for scanner.Scan() {
		line := scanner.Text()
		if value, ok := strings.CutPrefix(line, "NOOSPHERE_CODELENS_READY "); ok && !sent {
			if port, err := strconv.ParseUint(value, 10, 16); err == nil && port > 0 {
				reported <- int(port)
				sent = true
				continue
			}
		}
		log.Printf("[codelens] %s", line)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[codelens] worker stdout: %v", err)
		_, _ = io.Copy(io.Discard, stdout)
	}
}

func newHealthClient() *http.Client {
	return &http.Client{
		Timeout:       500 * time.Millisecond,
		Transport:     &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (a *Adapter) Stop() {
	if a.cmd != nil && a.cmd.Process != nil && a.done != nil {
		a.cmd.Process.Kill()
		<-a.done
		log.Println("[codelens] worker stopped")
	}
}

func (a *Adapter) WaitForExit(ctx context.Context) error {
	if a.done == nil {
		return fmt.Errorf("CodeLens 源码服务尚未启动")
	}
	interval := time.NewTicker(5 * time.Second)
	defer interval.Stop()
	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.done:
			if a.waitErr != nil {
				return fmt.Errorf("CodeLens 源码服务已退出: %w", a.waitErr)
			}
			return fmt.Errorf("CodeLens 源码服务已退出")
		case <-interval.C:
			if a.Healthy() {
				consecutiveFailures = 0
			} else {
				consecutiveFailures++
				if consecutiveFailures == 3 {
					return fmt.Errorf("CodeLens 源码服务健康检查连续失败")
				}
			}
		}
	}
}

func (a *Adapter) Healthy() bool {
	if a.proxy == nil {
		return false
	}
	addr := fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", a.port)
	req, _ := http.NewRequest("GET", addr, nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := newHealthClient().Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func (a *Adapter) Proxy() http.Handler {
	return a.proxy
}
