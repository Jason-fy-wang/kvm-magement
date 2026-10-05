package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type Config struct {
	ID         string
	VCPUCount  int
	MemoryMiB  int
	KernelPath string
	RootFSPath string
	DiskPath   string
	TapDevice  string
	GuestMAC   string
}

type Client struct {
	SocketPath string
	Process    *exec.Cmd
	HTTP       *http.Client
	ConsoleIn  io.WriteCloser
	ConsoleOut io.ReadCloser
}

func StartProcess(ctx context.Context, binary, socketPath string) (*Client, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return nil, fmt.Errorf("create runtime directory: %w", err)
	}
	_ = os.Remove(socketPath)
	cmd := exec.CommandContext(ctx, binary, "--api-sock", socketPath, "--enable-pci")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	consoleIn, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create firecracker stdin: %w", err)
	}
	consoleOut, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create firecracker stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start firecracker: %w", err)
	}

	client := &Client{SocketPath: socketPath, Process: cmd, ConsoleIn: consoleIn, ConsoleOut: consoleOut}
	client.HTTP = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}, Timeout: 30 * time.Second}

	startupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := waitForSocket(startupCtx, socketPath); err != nil {
		_ = cmd.Process.Kill()
		waitErr := cmd.Wait()
		stderrText := bytes.TrimSpace(stderr.Bytes())
		if len(stderrText) > 4096 {
			stderrText = stderrText[len(stderrText)-4096:]
		}
		if len(stderrText) > 0 {
			return nil, fmt.Errorf("firecracker API socket %s was not ready: %w; process_error=%v; stderr=%s", socketPath, err, waitErr, string(stderrText))
		}
		return nil, fmt.Errorf("firecracker API socket %s was not ready: %w; process_error=%v", socketPath, err, waitErr)
	}

	return client, nil
}

func waitForSocket(ctx context.Context, socketPath string) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		conn, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Client) Configure(ctx context.Context, cfg Config) error {
	requests := []struct {
		method string
		path   string
		body   any
	}{
		//ToDo: add output log for firecracker
		{"PUT", "/logger", map[string]any{"log_path": "/tmp/log-" + cfg.ID, "level": "Debug", "show_level": true, "show_log_origin": true}},
		{"PUT", "/machine-config", map[string]any{"vcpu_count": cfg.VCPUCount, "mem_size_mib": cfg.MemoryMiB}},
		{"PUT", "/boot-source", map[string]any{"kernel_image_path": cfg.KernelPath, "boot_args": "console=ttyS0 reboot=k panic=1"}},
		{"PUT", "/drives/rootfs", map[string]any{"drive_id": "rootfs", "path_on_host": cfg.RootFSPath, "is_root_device": true, "is_read_only": false}},
	}
	if cfg.DiskPath != "" {
		requests = append(requests, struct {
			method string
			path   string
			body   any
		}{"PUT", "/drives/data", map[string]any{"drive_id": "data", "path_on_host": cfg.DiskPath, "is_root_device": false, "is_read_only": false}})
	}
	if cfg.TapDevice != "" {
		network := map[string]any{"iface_id": "eth0", "host_dev_name": cfg.TapDevice}
		if cfg.GuestMAC != "" {
			network["guest_mac"] = cfg.GuestMAC
		}
		requests = append(requests, struct {
			method string
			path   string
			body   any
		}{"PUT", "/network-interfaces/eth0", network})
	}
	for _, request := range requests {
		if err := c.request(ctx, request.method, request.path, request.body); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Start(ctx context.Context) error {
	return c.request(ctx, "PUT", "/actions", map[string]string{"action_type": "InstanceStart"})
}

func (c *Client) SendCtrlAltDel(ctx context.Context) error {
	return c.request(ctx, "PUT", "/actions", map[string]string{"action_type": "SendCtrlAltDel"})
}

func (c *Client) GracefulShutdown(ctx context.Context) error {
	if c == nil || c.Process == nil || c.Process.Process == nil {
		return nil
	}

	// Prefer a guest-agent or console "poweroff" command here.
	// Ctrl+Alt+Del may reboot depending on guest configuration.
	if err := c.SendCtrlAltDel(ctx); err != nil {
		return fmt.Errorf("request guest shutdown: %w", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- c.Process.Wait()
	}()

	select {
	case <-waitCh:
		// The process exited. Its exit code is less important than the fact
		// that the guest had an opportunity to flush and shut down.
		return nil
	case <-ctx.Done():
	}

	if err := c.Process.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send Firecracker SIGTERM: %w", err)
	}

	select {
	case <-waitCh:
		return nil
	case <-time.After(3 * time.Second):
		_ = c.Process.Process.Kill()
		<-waitCh
		return fmt.Errorf("guest did not shut down gracefully")
	}
}

func (c *Client) Shutdown() error {
	if c == nil || c.Process == nil || c.Process.Process == nil {
		return nil
	}
	return c.Process.Process.Kill()
}

func (c *Client) request(ctx context.Context, method, path string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://firecracker"+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("firecracker %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("firecracker %s %s returned %s: %s", method, path, resp.Status, string(detail))
	}
	return nil
}
