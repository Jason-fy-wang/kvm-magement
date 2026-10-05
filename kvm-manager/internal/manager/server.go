package manager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Config struct {
	ListenAddr, DatabasePath, NetworkCIDR string
	Logger                                *slog.Logger
}

type Server struct {
	cfg       Config
	store     *store
	logger    *slog.Logger
	hub       *eventHub
	clientsMu sync.RWMutex
	clients   map[string]*websocket.Conn
	networkMu sync.Mutex
	network   *net.IPNet
	gatewayIP net.IP
}

func NewServer(cfg Config) (*Server, error) {
	if cfg.DatabasePath == "" {
		cfg.DatabasePath = "kvm-manager.db"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.NetworkCIDR == "" {
		cfg.NetworkCIDR = "172.20.0.0/16"
	}
	_, network, err := net.ParseCIDR(cfg.NetworkCIDR)
	if err != nil {
		return nil, fmt.Errorf("parse network CIDR: %w", err)
	}
	gateway := append(net.IP(nil), network.IP.To4()...)
	gateway[3]++
	s, err := openStore(cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("open manager database: %w", err)
	}
	return &Server{cfg: cfg, store: s, logger: cfg.Logger, hub: newEventHub(), clients: make(map[string]*websocket.Conn), network: network, gatewayIP: gateway}, nil
}

func (s *Server) Run(ctx context.Context) error {
	if s.cfg.ListenAddr == "" {
		s.cfg.ListenAddr = ":8080"
	}
	mux := http.NewServeMux()
	s.routes(mux)
	httpServer := &http.Server{Addr: s.cfg.ListenAddr, Handler: s.withRequestLogging(mux)}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		_ = s.store.close()
	}()
	s.logger.Info("kvm manager listening", "address", s.cfg.ListenAddr, "database", s.cfg.DatabasePath)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/healthz", s.health)
	mux.HandleFunc("GET /api/v1/agents", s.listAgents)
	mux.HandleFunc("GET /api/v1/vms", s.listVMs)
	mux.HandleFunc("POST /api/v1/vms", s.createVM)
	mux.HandleFunc("GET /api/v1/vms/{id}", s.getVM)
	mux.HandleFunc("DELETE /api/v1/vms/{id}", s.deleteVM)
	mux.HandleFunc("POST /api/v1/vms/{id}/start", s.lifecycle("start"))
	mux.HandleFunc("POST /api/v1/vms/{id}/stop", s.lifecycle("stop"))
	mux.HandleFunc("POST /api/v1/vms/{id}/reboot", s.lifecycle("reboot"))
	mux.HandleFunc("GET /api/v1/operations/{id}", s.getOperation)
	mux.HandleFunc("GET /api/v1/events", s.frontendEvents)
	mux.HandleFunc("GET /api/v1/vms/{id}/console", s.vmConsole)
	mux.HandleFunc("GET /ws/agents", s.agentWebSocket)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.listAgents(r.Context())
	if err != nil {
		s.logger.ErrorContext(r.Context(), "list agents failed", "error", err)
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agents)
}
func (s *Server) listVMs(w http.ResponseWriter, r *http.Request) {
	vms, err := s.store.listVMs(r.Context())
	if err != nil {
		s.logger.ErrorContext(r.Context(), "list VMs failed", "error", err)
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, vms)
}

func (s *Server) getVM(w http.ResponseWriter, r *http.Request) {
	vm, err := s.store.getVM(r.Context(), r.PathValue("id"))
	if errors.Is(err, errNotFound) {
		http.Error(w, "VM not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.ErrorContext(r.Context(), "get VM failed", "vm_id", r.PathValue("id"), "error", err)
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, vm)
}

func (s *Server) createVM(w http.ResponseWriter, r *http.Request) {
	var input CreateVMRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil {
		s.logger.WarnContext(r.Context(), "invalid create VM request", "error", err)
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if input.VCPUCount < 1 || input.MemoryMiB < 128 || input.KernelPath == "" || input.RootFSPath == "" {
		http.Error(w, "vcpu_count, memory_mib >= 128, kernel_path, and rootfs_path are required", http.StatusBadRequest)
		return
	}
	agent, err := s.selectAgent(r.Context(), input.AgentID, input.VCPUCount, input.DiskPath)
	if err != nil {
		s.logger.WarnContext(r.Context(), "no agent available for VM", "requested_agent_id", input.AgentID, "error", err)
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	id := input.ID
	if id == "" {
		id = newID("vm")
	}
	now := time.Now().UTC()
	allocation, err := s.allocateNetwork(r.Context(), id, agent.ID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "allocate VM network", "vm_id", id, "agent_id", agent.ID, "error", err)
		http.Error(w, "network allocation failed: "+err.Error(), http.StatusConflict)
		return
	}
	vm := VM{ID: id, Name: input.Name, AgentID: agent.ID, State: "creating", VCPUCount: input.VCPUCount, MemoryMiB: input.MemoryMiB, KernelPath: input.KernelPath, RootFSPath: input.RootFSPath, DiskPath: input.DiskPath, TapDevice: input.TapDevice, GuestIP: allocation.GuestIP, GuestMAC: allocation.GuestMAC, GatewayIP: allocation.GatewayIP, Netmask: allocation.Netmask, CreatedAt: now, UpdatedAt: now}
	if err := s.store.createVM(r.Context(), vm); err != nil {
		s.logger.ErrorContext(r.Context(), "persist VM failed", "vm_id", vm.ID, "error", err)
		_ = s.store.releaseIPAllocation(r.Context(), vm.ID)
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, "VM already exists", http.StatusConflict)
			return
		}
		serverError(w, err)
		return
	}
	managerOp := Operation{ID: newID("op"), VMID: vm.ID, AgentID: agent.ID, Type: "create", Status: "queued", CreatedAt: now, UpdatedAt: now}
	if err := s.store.createOperation(r.Context(), managerOp); err != nil {
		s.logger.ErrorContext(r.Context(), "persist VM operation failed", "vm_id", vm.ID, "operation_id", managerOp.ID, "error", err)
		serverError(w, err)
		return
	}
	var response struct {
		OperationID string `json:"operation_id"`
		VMID        string `json:"vm_id"`
		Status      string `json:"status"`
	}
	if err := s.agentRequest(r.Context(), agent, http.MethodPost, "/v1/vms", map[string]any{"id": vm.ID, "name": vm.Name, "vcpu_count": vm.VCPUCount, "memory_mib": vm.MemoryMiB, "kernel_path": vm.KernelPath, "rootfs_path": vm.RootFSPath, "disk_path": vm.DiskPath, "tap_device": vm.TapDevice, "guest_ip": vm.GuestIP, "guest_mac": vm.GuestMAC, "gateway_ip": vm.GatewayIP, "netmask": vm.Netmask}, &response); err != nil {
		s.logger.ErrorContext(r.Context(), "create VM on agent failed", "vm_id", vm.ID, "agent_id", agent.ID, "operation_id", managerOp.ID, "error", err)
		_ = s.store.updateOperation(r.Context(), managerOp.ID, "failed", err.Error())
		_ = s.store.updateVMState(r.Context(), vm.ID, "error", time.Now().UTC())
		_ = s.store.releaseIPAllocation(r.Context(), vm.ID)
		http.Error(w, "agent request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if err := s.store.bindAgentOperation(r.Context(), managerOp.ID, response.OperationID); err != nil {
		s.logger.ErrorContext(r.Context(), "bind agent operation failed", "operation_id", managerOp.ID, "agent_operation_id", response.OperationID, "error", err)
		serverError(w, err)
		return
	}
	managerOp.AgentOperationID, managerOp.Status = response.OperationID, "running"
	s.logger.InfoContext(r.Context(), "VM create accepted by agent", "vm_id", vm.ID, "agent_id", agent.ID, "guest_ip", vm.GuestIP, "guest_mac", vm.GuestMAC, "operation_id", managerOp.ID, "agent_operation_id", response.OperationID)
	s.publish(Event{Type: "operation.updated", AgentID: agent.ID, VMID: vm.ID, Operation: &managerOp, Timestamp: time.Now().UTC()})
	writeJSON(w, http.StatusAccepted, map[string]any{"operation_id": managerOp.ID, "vm_id": vm.ID, "status": "running"})
}

func (s *Server) deleteVM(w http.ResponseWriter, r *http.Request) { s.lifecycle("delete")(w, r) }

func (s *Server) lifecycle(operationType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vm, err := s.store.getVM(r.Context(), r.PathValue("id"))
		if errors.Is(err, errNotFound) {
			http.Error(w, "VM not found", http.StatusNotFound)
			return
		}
		if err != nil {
			s.logger.WarnContext(r.Context(), "VM not found for lifecycle operation", "vm_id", r.PathValue("id"), "operation", operationType)
			serverError(w, err)
			return
		}
		agent, err := s.store.getAgent(r.Context(), vm.AgentID)
		if err != nil {
			s.logger.WarnContext(r.Context(), "agent unavailable for lifecycle operation", "vm_id", vm.ID, "agent_id", vm.AgentID, "operation", operationType, "error", err)
			http.Error(w, "agent not found", http.StatusConflict)
			return
		}
		managerOp := Operation{ID: newID("op"), VMID: vm.ID, AgentID: vm.AgentID, Type: operationType, Status: "queued", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		if err := s.store.createOperation(r.Context(), managerOp); err != nil {
			serverError(w, err)
			return
		}
		var response struct {
			OperationID string `json:"operation_id"`
		}
		path := "/v1/vms/" + vm.ID + "/" + operationType
		if operationType == "delete" {
			path = "/v1/vms/" + vm.ID
		}
		method := http.MethodPost
		if operationType == "delete" {
			method = http.MethodDelete
		}
		if err := s.agentRequest(r.Context(), agent, method, path, nil, &response); err != nil {
			s.logger.ErrorContext(r.Context(), "agent lifecycle request failed", "vm_id", vm.ID, "agent_id", agent.ID, "operation", operationType, "error", err)
			_ = s.store.updateOperation(r.Context(), managerOp.ID, "failed", err.Error())
			http.Error(w, "agent request failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		if err := s.store.bindAgentOperation(r.Context(), managerOp.ID, response.OperationID); err != nil {
			serverError(w, err)
			return
		}
		managerOp.AgentOperationID, managerOp.Status = response.OperationID, "running"
		s.logger.InfoContext(r.Context(), "VM lifecycle operation accepted", "vm_id", vm.ID, "agent_id", agent.ID, "operation", operationType, "operation_id", managerOp.ID, "agent_operation_id", response.OperationID)
		s.publish(Event{Type: "operation.updated", AgentID: agent.ID, VMID: vm.ID, Operation: &managerOp, Timestamp: time.Now().UTC()})
		writeJSON(w, http.StatusAccepted, map[string]any{"operation_id": managerOp.ID, "vm_id": vm.ID, "status": "running"})
	}
}

func (s *Server) getOperation(w http.ResponseWriter, r *http.Request) {
	op, err := s.store.getOperation(r.Context(), r.PathValue("id"))
	if errors.Is(err, errNotFound) {
		http.Error(w, "operation not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.ErrorContext(r.Context(), "get operation failed", "operation_id", r.PathValue("id"), "error", err)
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, op)
}

func (s *Server) vmConsole(w http.ResponseWriter, r *http.Request) {
	vm, err := s.store.getVM(r.Context(), r.PathValue("id"))
	if errors.Is(err, errNotFound) {
		http.Error(w, "VM not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.WarnContext(r.Context(), "agent console connection failed", "vm_id", vm.ID, "agent_id", vm.AgentID, "error", err)
		serverError(w, err)
		return
	}
	agent, err := s.store.getAgent(r.Context(), vm.AgentID)
	if err != nil || agent.URL == "" {
		http.Error(w, "agent console is not available", http.StatusConflict)
		return
	}

	backendSchemeURL := agent.URL
	if strings.HasPrefix(backendSchemeURL, "https://") {
		backendSchemeURL = "wss://" + strings.TrimPrefix(backendSchemeURL, "https://")
	} else {
		backendSchemeURL = "ws://" + strings.TrimPrefix(backendSchemeURL, "http://")
	}
	backendURL := strings.TrimRight(backendSchemeURL, "/") + "/v1/vms/" + vm.ID + "/console"
	backend, _, err := websocket.DefaultDialer.DialContext(r.Context(), backendURL, nil)
	if err != nil {
		http.Error(w, "agent console connection failed", http.StatusBadGateway)
		return
	}
	defer backend.Close()
	frontend, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer frontend.Close()

	finished := make(chan struct{}, 2)
	bridge := func(dst, src *websocket.Conn) {
		defer func() { finished <- struct{}{} }()
		for {
			messageType, payload, readErr := src.ReadMessage()
			if readErr != nil {
				return
			}
			if writeErr := dst.WriteMessage(messageType, payload); writeErr != nil {
				return
			}
		}
	}
	go bridge(backend, frontend)
	go bridge(frontend, backend)
	<-finished
}

func (s *Server) selectAgent(ctx context.Context, requested string, vcpus int, diskPath string) (Agent, error) {
	if requested != "" {
		a, err := s.store.getAgent(ctx, requested)
		if errors.Is(err, errNotFound) {
			return Agent{}, errors.New("requested agent not found")
		}
		if err != nil {
			return Agent{}, err
		}
		if a.Status != "online" {
			return Agent{}, errors.New("requested agent is offline")
		}
		s.logger.DebugContext(ctx, "selected requested agent", "agent_id", a.ID, "vcpu_available", a.VCPUAvailable, "disk_free_bytes", a.DiskFreeBytes)
		return a, nil
	}
	agents, err := s.store.listAgents(ctx)
	if err != nil {
		return Agent{}, err
	}
	var selected Agent
	selectedScore := int64(-1)
	diskNeed := int64(0)
	if diskPath != "" {
		diskNeed = 1
	}
	for _, a := range agents {
		if a.Status != "online" {
			continue
		}
		if a.VCPUAvailable > 0 && a.VCPUAvailable < vcpus {
			continue
		}
		if diskNeed > 0 && a.DiskFreeBytes > 0 && a.DiskFreeBytes < diskNeed {
			continue
		}
		score := int64(a.VCPUAvailable)*1_000_000 + a.DiskFreeBytes
		if selectedScore < 0 || score > selectedScore {
			selected, selectedScore = a, score
		}
	}
	if selectedScore < 0 {
		return Agent{}, errors.New("no online agent has sufficient capacity")
	}
	s.logger.DebugContext(ctx, "selected agent by capacity", "agent_id", selected.ID, "vcpu_available", selected.VCPUAvailable, "disk_free_bytes", selected.DiskFreeBytes)
	return selected, nil
}

func (s *Server) agentRequest(ctx context.Context, agent Agent, method, path string, body any, output any) error {
	if agent.URL == "" {
		return errors.New("agent has no HTTP URL")
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(payload))
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(agent.URL, "/")+path, reader)
	if err != nil {
		return err
	}
	started := time.Now()
	s.logger.DebugContext(ctx, "calling agent API", "agent_id", agent.ID, "method", method, "path", path)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.logger.ErrorContext(ctx, "agent API request failed", "agent_id", agent.ID, "method", method, "path", path, "duration_ms", time.Since(started).Milliseconds(), "error", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err := fmt.Errorf("agent returned %s: %s", resp.Status, string(detail))
		s.logger.ErrorContext(ctx, "agent API returned error", "agent_id", agent.ID, "method", method, "path", path, "status", resp.StatusCode, "duration_ms", time.Since(started).Milliseconds(), "error", err)
		return err
	}
	s.logger.DebugContext(ctx, "agent API request completed", "agent_id", agent.ID, "method", method, "path", path, "status", resp.StatusCode, "duration_ms", time.Since(started).Milliseconds())
	return json.NewDecoder(resp.Body).Decode(output)
}

var wsUpgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func (s *Server) agentWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	var registration struct {
		Type, AgentID, Hostname, URL  string
		VCPUTotal, VCPUAvailable      int
		DiskTotalBytes, DiskFreeBytes int64
	}
	if err := conn.ReadJSON(&registration); err != nil || registration.Type != "agent.register" || registration.AgentID == "" {
		return
	}
	if registration.Hostname == "" {
		registration.Hostname = registration.AgentID
	}
	now := time.Now().UTC()
	agent := Agent{ID: registration.AgentID, Hostname: registration.Hostname, URL: registration.URL, Status: "online", VCPUTotal: registration.VCPUTotal, VCPUAvailable: registration.VCPUAvailable, DiskTotalBytes: registration.DiskTotalBytes, DiskFreeBytes: registration.DiskFreeBytes, LastSeen: now}
	if err := s.store.upsertAgent(r.Context(), agent); err != nil {
		s.logger.Error("persist agent registration", "error", err)
		return
	}
	s.logger.InfoContext(r.Context(), "agent registered", "agent_id", agent.ID, "hostname", agent.Hostname, "url", agent.URL, "vcpu_total", agent.VCPUTotal, "vcpu_available", agent.VCPUAvailable, "disk_free_bytes", agent.DiskFreeBytes)
	s.clientsMu.Lock()
	s.clients[agent.ID] = conn
	s.clientsMu.Unlock()
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, agent.ID)
		s.clientsMu.Unlock()
		_ = s.store.setAgentStatus(context.Background(), agent.ID, "offline", time.Now().UTC())
		s.logger.Warn("agent disconnected", "agent_id", agent.ID)
	}()
	for {
		var event Event
		if err := conn.ReadJSON(&event); err != nil {
			s.logger.Warn("agent event stream ended", "agent_id", agent.ID, "error", err)
			return
		}
		s.logger.Debug("agent event received", "agent_id", agent.ID, "type", event.Type, "vm_id", event.VMID)
		_ = s.handleAgentEvent(r.Context(), agent.ID, event)
	}
}

func (s *Server) handleAgentEvent(ctx context.Context, agentID string, event Event) error {
	if event.Type == "agent.heartbeat" {
		return s.store.setAgentStatus(ctx, agentID, "online", time.Now().UTC())
	}
	if event.Operation != nil {
		if op, err := s.store.findOperationByAgent(ctx, agentID, event.Operation.ID); err == nil {
			_ = s.store.updateOperation(ctx, op.ID, event.Operation.Status, event.Operation.Error)
			s.logger.InfoContext(ctx, "operation status updated", "operation_id", op.ID, "agent_operation_id", event.Operation.ID, "vm_id", op.VMID, "status", event.Operation.Status, "error", event.Operation.Error)
			if event.Operation.Status == "succeeded" {
				_ = s.store.updateVMState(ctx, op.VMID, stateForOperation(op.Type), time.Now().UTC())
				if op.Type == "delete" {
					_ = s.store.releaseIPAllocation(ctx, op.VMID)
				}
			}
		} else if !errors.Is(err, errNotFound) {
			s.logger.ErrorContext(ctx, "find operation from agent event failed", "agent_id", agentID, "agent_operation_id", event.Operation.ID, "error", err)
		}
	}
	if event.VM != nil {
		_ = s.store.updateVMState(ctx, event.VM.ID, event.VM.State, time.Now().UTC())
	}
	s.publish(Event{Type: event.Type, AgentID: agentID, VMID: event.VMID, Operation: event.Operation, VM: event.VM, Timestamp: time.Now().UTC()})
	return nil
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
		// WebSocket upgrades need the original http.Hijacker implementation.
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
		s.logger.InfoContext(r.Context(), "HTTP request completed", "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(started).Milliseconds(), "remote", r.RemoteAddr)
	})
}

func stateForOperation(operationType string) string {
	switch operationType {
	case "create", "stop":
		return "stopped"
	case "start", "reboot":
		return "running"
	case "delete":
		return "deleted"
	default:
		return "unknown"
	}
}

func (s *Server) frontendEvents(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	sub := s.hub.subscribe()
	defer s.hub.unsubscribe(sub)
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-sub:
			if err := conn.WriteJSON(event); err != nil {
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
func serverError(w http.ResponseWriter, err error) {
	http.Error(w, "internal server error: "+err.Error(), http.StatusInternalServerError)
}
func newID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b)
}
