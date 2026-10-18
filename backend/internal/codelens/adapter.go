package codelens

import (
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
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
	if len(a.cmdArgs) < 2 || a.cmdArgs[0] == "" {
		return fmt.Errorf("codelens Python command is empty")
	}
	probe, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", a.port))
	if err != nil {
		return fmt.Errorf("codelens port %d is occupied: %w", a.port, err)
	}
	probe.Close()
	a.cmd = exec.Command(a.cmdArgs[0], a.cmdArgs[1:]...)
	a.cmd.Env = append(os.Environ(), fmt.Sprintf("NOOSPHERE_CODELENS_PORT=%d", a.port), "NOOSPHERE_CODELENS_TOKEN="+a.token)
	configureWorker(a.cmd)
	log.Printf("[codelens] starting Python worker: %v", a.cmdArgs)
	if err := a.cmd.Start(); err != nil {
		return fmt.Errorf("start codelens: %w", err)
	}
	a.done = make(chan struct{})
	go func() {
		a.waitErr = a.cmd.Wait()
		close(a.done)
	}()
	defer func() {
		if err != nil {
			a.Stop()
		}
	}()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	addr := fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", a.port)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-a.done:
			return fmt.Errorf("codelens Python worker exited before ready: %v", a.waitErr)
		default:
		}
		req, _ := http.NewRequest("GET", addr, nil)
		req.Header.Set("Authorization", "Bearer "+a.token)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		if err == nil && resp.StatusCode == http.StatusOK {
			target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", a.port))
			a.proxy = &httputil.ReverseProxy{
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
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("codelens worker not healthy after 15s")
}

func (a *Adapter) Stop() {
	if a.cmd != nil && a.cmd.Process != nil && a.done != nil {
		a.cmd.Process.Kill()
		<-a.done
		log.Println("[codelens] worker stopped")
	}
}

func (a *Adapter) Healthy() bool {
	if a.proxy == nil {
		return false
	}
	addr := fmt.Sprintf("http://127.0.0.1:%d/api/v1/health", a.port)
	req, _ := http.NewRequest("GET", addr, nil)
	req.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := (&http.Client{Timeout: 500 * time.Millisecond}).Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func (a *Adapter) Proxy() http.Handler {
	return a.proxy
}
