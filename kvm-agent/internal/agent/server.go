package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Jason-fy-wang/kvm-agent/internal/firecracker"
	"github.com/gorilla/websocket"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

type Config struct {
	ID          string
	ListenAddr  string
	ManagerURL  string
	PublicURL   string
	Firecracker string
	RuntimeDir  string
	Logger      *slog.Logger
}

type Server struct {
	cfg       Config
	store     *store
	clientsMu sync.Mutex
	clients   map[string]*firecracker.Client
	hub       *eventHub
	manager   *managerClient
}

func NewServer(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{cfg: cfg, store: newStore(), clients: make(map[string]*firecracker.Client), hub: newEventHub()}
}

func (s *Server) Run(ctx context.Context) error {
	if s.cfg.ID == "" {
		return errors.New("agent ID is required")
	}
	if s.cfg.ListenAddr == "" {
		s.cfg.ListenAddr = ":9090"
	}
	if s.cfg.RuntimeDir == "" {
		s.cfg.RuntimeDir = "/var/lib/kvm-agent"
	}
	if err := os.MkdirAll(s.cfg.RuntimeDir, 0o755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}

	mux := http.NewServeMux()
	s.routes(mux)
	httpServer := &http.Server{Addr: s.cfg.ListenAddr, Handler: s.withRequestLogging(mux)}
	if s.cfg.ManagerURL != "" {
		s.manager = newManagerClient(s.cfg.ManagerURL, s.cfg.ID, s.cfg.PublicURL, s.hub, s.cfg.Logger)
		go s.manager.Run(ctx)
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	s.cfg.Logger.Info("kvm agent listening", "agent_id", s.cfg.ID, "address", s.cfg.ListenAddr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// @title Swagger API for KVM Agent
// @version 1.0
// @description This is a KVM Agent server.
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @BasePath /v1
func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/healthz", s.health)
	mux.HandleFunc("GET /v1/agent", s.agentInfo)
	mux.HandleFunc("GET /v1/vms", s.listVMs)
	mux.HandleFunc("POST /v1/vms", s.createVM)
	mux.HandleFunc("GET /v1/vms/{id}", s.getVM)
	mux.HandleFunc("DELETE /v1/vms/{id}", s.deleteVM)
	mux.HandleFunc("POST /v1/vms/{id}/start", s.startVM)
	mux.HandleFunc("POST /v1/vms/{id}/stop", s.stopVM)
	mux.HandleFunc("POST /v1/vms/{id}/reboot", s.rebootVM)
	mux.HandleFunc("GET /v1/operations/{id}", s.getOperation)
	mux.HandleFunc("GET /v1/vms/{id}/console", s.console)
	mux.HandleFunc("GET /v1/events", s.events)

	// swagger
	mux.HandleFunc("/swagger/", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"), // The url pointing to API definition
	))
}

// health godoc
// @Summary Health check
// @Description Returns the health status of the KVM Agent.
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]string
// @Router /healthz [get]
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// agentInfo godoc
// @Summary Agent information
// @Description Returns the agent ID, hostname, and version.
// @Tags Agent
// @Produce json
// @Success 200 {object} map[string]string
// @Router /agent [get]
func (s *Server) agentInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"id": s.cfg.ID, "hostname": s.cfg.ID, "version": "0.1.0"})
}

// listVMs godoc
// @Summary List VMs
// @Description Returns a list of all VMs managed by the agent.
// @Tags VMs
// @Produce json
// @Success 200 {array} VM
// @Router /vms [get]
func (s *Server) listVMs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.listVMs())
}

// getVM godoc
// @Summary Get VM
// @Description Returns the details of a specific VM by ID.
// @Tags VMs
// @Produce json
// @Param id path string true "VM ID"
// @Success 200 {object} VM
// @Failure 404 {object} map[string]string
// @Router /vms/{id} [get]
func (s *Server) getVM(w http.ResponseWriter, r *http.Request) {
	vm, err := s.store.getVM(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, vm)
}

// createVM godoc
// @Summary Create VM
// @Description Creates a new VM with the specified configuration.
// @Tags VMs
// @Accept json
// @Produce json
// @Param vm body CreateVMRequest true "VM configuration"
// @Success 202 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /vms [post]
func (s *Server) createVM(w http.ResponseWriter, r *http.Request) {
	var input CreateVMRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.cfg.Logger.WarnContext(r.Context(), "invalid create VM request", "error", err)
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if input.VCPUCount < 1 || input.MemoryMiB < 128 || input.KernelPath == "" || input.RootFSPath == "" {
		http.Error(w, "vcpu_count, memory_mib >= 128, kernel_path, and rootfs_path are required", http.StatusBadRequest)
		return
	}
	if input.ID == "" {
		input.ID = newID("vm")
	}
	if _, err := s.store.getVM(input.ID); err == nil {
		s.cfg.Logger.WarnContext(r.Context(), "create VM rejected because VM already exists", "vm_id", input.ID)
		http.Error(w, "VM already exists", http.StatusConflict)
		return
	}
	now := time.Now().UTC()
	vm := &VM{ID: input.ID, Name: input.Name, State: VMStateCreating, VCPUCount: input.VCPUCount, MemoryMiB: input.MemoryMiB, KernelPath: input.KernelPath, RootFSPath: input.RootFSPath, DiskPath: input.DiskPath, TapDevice: input.TapDevice, GuestIP: input.GuestIP, GuestMAC: input.GuestMAC, GatewayIP: input.GatewayIP, Netmask: input.Netmask, CreatedAt: now, UpdatedAt: now}
	s.store.putVM(vm)
	op := s.newOperation(vm.ID, "create")
	s.cfg.Logger.InfoContext(r.Context(), "VM create accepted", "vm_id", vm.ID, "operation_id", op.ID, "vcpu_count", vm.VCPUCount, "memory_mib", vm.MemoryMiB, "tap_device", vm.TapDevice)
	s.runAsync(op, func(ctx context.Context) error {
		client, err := s.startFirecracker(ctx, vm)
		if err != nil {
			s.cfg.Logger.Error("Firecracker startup/configuration failed", "vm_id", vm.ID, "operation_id", op.ID, "error", err)
			vm.State = VMStateError
			vm.UpdatedAt = time.Now().UTC()
			s.store.putVM(vm)
			return err
		}
		s.clientsMu.Lock()
		s.clients[vm.ID] = client
		s.clientsMu.Unlock()
		vm.State = VMStateStopped
		vm.FirecrackerPID = client.Process.Process.Pid
		vm.UpdatedAt = time.Now().UTC()
		s.store.putVM(vm)
		s.cfg.Logger.Info("VM created and Firecracker configured", "vm_id", vm.ID, "operation_id", op.ID, "pid", vm.FirecrackerPID, "socket", client.SocketPath)
		return nil
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"operation_id": op.ID, "vm_id": vm.ID, "status": "queued"})
}

// deleteVM godoc
// @Summary Delete VM
// @Description Deletes an existing VM by ID.
// @Tags VMs
// @Produce json
// @Param id path string true "VM ID"
// @Success 202 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /vms/{id} [delete]
func (s *Server) deleteVM(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	vm, err := s.store.getVM(id)
	if err != nil {
		http.Error(w, "VM not found", http.StatusNotFound)
		return
	}
	op := s.newOperation(id, "delete")
	s.cfg.Logger.InfoContext(r.Context(), "VM delete accepted", "vm_id", id, "operation_id", op.ID)
	s.runAsync(op, func(_ context.Context) error {
		s.clientsMu.Lock()
		client := s.clients[id]
		delete(s.clients, id)
		s.clientsMu.Unlock()
		if client != nil {
			if shutdownErr := client.Shutdown(); shutdownErr != nil {
				s.cfg.Logger.Warn("Firecracker shutdown failed", "vm_id", id, "operation_id", op.ID, "error", shutdownErr)
			}
		}
		// The VM's kernel, rootfs, and disk are intentionally retained.
		return s.store.deleteVM(vm.ID)
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"operation_id": op.ID, "vm_id": id, "status": op.Status})
}

// startVM godoc
// @Summary Start VM
// @Description Starts a stopped VM by ID.
// @Tags VMs
// @Produce json
// @Param id path string true "VM ID"
// @Success 202 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /vms/{id}/start [post]
func (s *Server) startVM(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	vm, err := s.store.getVM(id)
	if err != nil {
		http.Error(w, "VM not found", http.StatusNotFound)
		return
	}
	op := s.newOperation(id, "start")
	s.cfg.Logger.InfoContext(r.Context(), "VM start accepted", "vm_id", id, "operation_id", op.ID)
	s.runAsync(op, func(ctx context.Context) error {
		s.clientsMu.Lock()
		client := s.clients[id]
		s.clientsMu.Unlock()
		if client == nil {
			s.cfg.Logger.Debug("no existing Firecracker process; starting a new one", "vm_id", id, "operation_id", op.ID)
			client, err = s.startFirecracker(ctx, vm)
			if err != nil {
				return err
			}
			s.clientsMu.Lock()
			s.clients[id] = client
			s.clientsMu.Unlock()
		}
		if err := client.Start(ctx); err != nil {
			s.cfg.Logger.Error("Firecracker start action failed", "vm_id", id, "operation_id", op.ID, "error", err)
			return err
		}
		vm.State = VMStateRunning
		vm.FirecrackerPID = client.Process.Process.Pid
		vm.UpdatedAt = time.Now().UTC()
		s.store.putVM(vm)
		s.cfg.Logger.Info("VM is running", "vm_id", id, "operation_id", op.ID, "pid", vm.FirecrackerPID)
		return nil
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"operation_id": op.ID, "vm_id": id, "status": "queued"})
}

// stopVM godoc
// @Summary Stop VM
// @Description Stops a running VM by ID.
// @Tags VMs
// @Produce json
// @Param id path string true "VM ID"
// @Success 202 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /vms/{id}/stop [post]
func (s *Server) stopVM(w http.ResponseWriter, r *http.Request) {
	s.lifecycle(w, r, "stop", VMStateStopped, func(ctx context.Context, c *firecracker.Client) error {
		shutdownCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return c.GracefulShutdown(shutdownCtx)
	})
}

// rebootVM godoc
// @Summary Reboot VM
// @Description Reboots a running VM by ID.
// @Tags VMs
// @Produce json
// @Param id path string true "VM ID"
// @Success 202 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /vms/{id}/reboot [post]
func (s *Server) rebootVM(w http.ResponseWriter, r *http.Request) {
	s.lifecycle(w, r, "reboot", VMStateRunning, func(ctx context.Context, c *firecracker.Client) error { return c.SendCtrlAltDel(ctx) })
}

func (s *Server) lifecycle(w http.ResponseWriter, r *http.Request, operationType string, target VMState, action func(context.Context, *firecracker.Client) error) {
	id := r.PathValue("id")
	vm, err := s.store.getVM(id)
	if err != nil {
		http.Error(w, "VM not found", http.StatusNotFound)
		return
	}
	s.clientsMu.Lock()
	client := s.clients[id]
	s.clientsMu.Unlock()
	if client == nil {
		http.Error(w, "VM process is not available", http.StatusConflict)
		return
	}
	op := s.newOperation(id, operationType)
	s.cfg.Logger.InfoContext(r.Context(), "VM lifecycle operation accepted", "vm_id", id, "operation_id", op.ID, "operation", operationType)
	s.runAsync(op, func(ctx context.Context) error {
		if err := action(ctx, client); err != nil {
			s.cfg.Logger.Error("Firecracker lifecycle action failed", "vm_id", id, "operation_id", op.ID, "operation", operationType, "error", err)
			return err
		}
		if operationType == "stop" {
			s.clientsMu.Lock()
			delete(s.clients, id)
			s.clientsMu.Unlock()
		}
		vm.State = target
		vm.UpdatedAt = time.Now().UTC()
		s.store.putVM(vm)
		s.cfg.Logger.Info("VM lifecycle operation completed", "vm_id", id, "operation_id", op.ID, "operation", operationType, "state", target)
		return nil
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"operation_id": op.ID, "vm_id": id, "status": "queued"})
}

func (s *Server) startFirecracker(ctx context.Context, vm *VM) (*firecracker.Client, error) {
	socketPath := filepath.Join(s.cfg.RuntimeDir, vm.ID+".sock")
	s.cfg.Logger.DebugContext(ctx, "starting Firecracker", "vm_id", vm.ID, "binary", s.cfg.Firecracker, "socket", socketPath)
	client, err := firecracker.StartProcess(ctx, s.cfg.Firecracker, socketPath)
	if err != nil {
		s.cfg.Logger.ErrorContext(ctx, "start process error:", err)
		return nil, err
	}
	if err := client.Configure(ctx, firecracker.Config{ID: vm.ID, VCPUCount: vm.VCPUCount, MemoryMiB: vm.MemoryMiB, KernelPath: vm.KernelPath, RootFSPath: vm.RootFSPath, DiskPath: vm.DiskPath, TapDevice: vm.TapDevice, GuestMAC: vm.GuestMAC}); err != nil {
		s.cfg.Logger.ErrorContext(ctx, "configure Firecracker failed", "vm_id", vm.ID, "socket", socketPath, "error", err)
		_ = client.Shutdown()
		return nil, err
	}
	return client, nil
}

// getOperation godoc
// @Summary Get Operation
// @Description Returns the details of a specific operation by ID.
// @Tags Operations
// @Produce json
// @Param id path string true "Operation ID"
// @Success 200 {object} Operation
// @Failure 404 {object} map[string]string
// @Router /operations/{id} [get]
func (s *Server) getOperation(w http.ResponseWriter, r *http.Request) {
	op, err := s.store.getOperation(r.PathValue("id"))
	if err != nil {
		http.Error(w, "operation not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, op)
}

func (s *Server) newOperation(vmID, operationType string) *Operation {
	now := time.Now().UTC()
	op := &Operation{ID: newID("op"), VMID: vmID, Type: operationType, Status: "queued", CreatedAt: now, UpdatedAt: now}
	s.store.putOperation(op)
	s.publish(Event{Type: "operation.created", VMID: vmID, Operation: op, Timestamp: now})
	return op
}

func (s *Server) runAsync(op *Operation, fn func(context.Context) error) {
	go func() {
		op.Status, op.UpdatedAt = "running", time.Now().UTC()
		s.cfg.Logger.Debug("operation started", "operation_id", op.ID, "vm_id", op.VMID, "operation", op.Type)
		s.store.putOperation(op)
		s.publish(Event{Type: "operation.updated", VMID: op.VMID, Operation: op, Timestamp: op.UpdatedAt})
		err := fn(context.Background())
		op.UpdatedAt = time.Now().UTC()
		if err != nil {
			op.Status, op.Error = "failed", err.Error()
			s.cfg.Logger.Error("operation failed", "operation_id", op.ID, "vm_id", op.VMID, "operation", op.Type, "error", err)
		} else {
			op.Status = "succeeded"
			s.cfg.Logger.Info("operation completed", "operation_id", op.ID, "vm_id", op.VMID, "operation", op.Type)
		}
		s.store.putOperation(op)
		s.publish(Event{Type: "operation.updated", VMID: op.VMID, Operation: op, Timestamp: op.UpdatedAt})
		if vm, getErr := s.store.getVM(op.VMID); getErr == nil {
			s.publish(Event{Type: "vm.status_changed", VMID: vm.ID, VM: vm, Timestamp: time.Now().UTC()})
		}
	}()
}

func (s *Server) publish(event Event) { s.hub.publish(event) }

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(payload)
}

func (s *Server) withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebSocket upgrades require the original http.Hijacker implementation.
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		wrapped := &loggingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)
		status := wrapped.status
		if status == 0 {
			status = http.StatusOK
		}
		s.cfg.Logger.InfoContext(r.Context(), "HTTP request completed", "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(started).Milliseconds(), "remote", r.RemoteAddr)
	})
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)
	defer conn.Close()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			if err := conn.WriteJSON(event); err != nil {
				return
			}
		}
	}
}

// console godoc
// @Summary VM Console
// @Description Opens a WebSocket connection to the console of a specific VM by ID.
// @Tags VMs
// @Produce json
// @Param id path string true "VM ID"
// @Success 101 {string} string "Switching Protocols"
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /vms/{id}/console [get]
func (s *Server) console(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.clientsMu.Lock()
	client := s.clients[id]
	s.clientsMu.Unlock()
	if client == nil || client.ConsoleOut == nil || client.ConsoleIn == nil {
		s.cfg.Logger.WarnContext(r.Context(), "console requested for unavailable VM", "vm_id", id)
		http.Error(w, "VM console is not available", http.StatusConflict)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.cfg.Logger.WarnContext(r.Context(), "console WebSocket upgrade failed", "vm_id", id, "error", err)
		return
	}
	defer conn.Close()
	s.cfg.Logger.InfoContext(r.Context(), "console connected", "vm_id", id)
	done := make(chan struct{})
	defer close(done)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := client.ConsoleOut.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.TextMessage, buf[:n]); writeErr != nil {
					s.cfg.Logger.Debug("console output connection closed", "vm_id", id, "error", writeErr)
					return
				}
			}
			if readErr != nil {
				s.cfg.Logger.Debug("Firecracker console output ended", "vm_id", id, "error", readErr)
				return
			}
		}
	}()
	for {
		messageType, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			s.cfg.Logger.Debug("console input connection closed", "vm_id", id, "error", readErr)
			return
		}
		if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage {
			if _, err := client.ConsoleIn.Write(payload); err != nil {
				s.cfg.Logger.Warn("write to Firecracker console failed", "vm_id", id, "error", err)
				return
			}
		}
	}
}

type eventHub struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
}

func newEventHub() *eventHub { return &eventHub{subscribers: make(map[chan Event]struct{})} }
func (h *eventHub) subscribe() chan Event {
	ch := make(chan Event, 32)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}
func (h *eventHub) unsubscribe(ch chan Event) {
	h.mu.Lock()
	delete(h.subscribers, ch)
	close(ch)
	h.mu.Unlock()
}
func (h *eventHub) publish(event Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func newID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return prefix + "-" + fmt.Sprint(time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b)
}
func trimURL(value string) string { return strings.TrimRight(value, "/") }
