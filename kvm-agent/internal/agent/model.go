package agent

import "time"

type VMState string

const (
	VMStateCreating VMState = "creating"
	VMStateStopped  VMState = "stopped"
	VMStateRunning  VMState = "running"
	VMStateStopping VMState = "stopping"
	VMStateError    VMState = "error"
)

type VM struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	State          VMState   `json:"state"`
	VCPUCount      int       `json:"vcpu_count"`
	MemoryMiB      int       `json:"memory_mib"`
	KernelPath     string    `json:"kernel_path"`
	RootFSPath     string    `json:"rootfs_path"`
	DiskPath       string    `json:"disk_path,omitempty"`
	TapDevice      string    `json:"tap_device,omitempty"`
	FirecrackerPID int       `json:"firecracker_pid,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CreateVMRequest struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	VCPUCount  int    `json:"vcpu_count"`
	MemoryMiB  int    `json:"memory_mib"`
	KernelPath string `json:"kernel_path"`
	RootFSPath string `json:"rootfs_path"`
	DiskPath   string `json:"disk_path"`
	TapDevice  string `json:"tap_device"`
}

type Operation struct {
	ID        string    `json:"id"`
	VMID      string    `json:"vm_id"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Event struct {
	Type      string     `json:"type"`
	AgentID   string     `json:"agent_id"`
	VMID      string     `json:"vm_id,omitempty"`
	Operation *Operation `json:"operation,omitempty"`
	VM        *VM        `json:"vm,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
}
