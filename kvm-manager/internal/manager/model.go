package manager

import "time"

type Agent struct {
	ID             string    `json:"id"`
	Hostname       string    `json:"hostname"`
	URL            string    `json:"url"`
	Status         string    `json:"status"`
	VCPUTotal      int       `json:"vcpu_total"`
	VCPUAvailable  int       `json:"vcpu_available"`
	DiskTotalBytes int64     `json:"disk_total_bytes"`
	DiskFreeBytes  int64     `json:"disk_free_bytes"`
	LastSeen       time.Time `json:"last_seen"`
}

type VM struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	AgentID    string    `json:"agent_id"`
	State      string    `json:"state"`
	VCPUCount  int       `json:"vcpu_count"`
	MemoryMiB  int       `json:"memory_mib"`
	KernelPath string    `json:"kernel_path"`
	RootFSPath string    `json:"rootfs_path"`
	DiskPath   string    `json:"disk_path,omitempty"`
	TapDevice  string    `json:"tap_device,omitempty"`
	GuestIP    string    `json:"guest_ip"`
	GuestMAC   string    `json:"guest_mac"`
	GatewayIP  string    `json:"gateway_ip"`
	Netmask    string    `json:"netmask"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CreateVMRequest struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AgentID    string `json:"agent_id"`
	VCPUCount  int    `json:"vcpu_count"`
	MemoryMiB  int    `json:"memory_mib"`
	KernelPath string `json:"kernel_path"`
	RootFSPath string `json:"rootfs_path"`
	DiskPath   string `json:"disk_path"`
	TapDevice  string `json:"tap_device"`
}

type Operation struct {
	ID               string    `json:"id"`
	VMID             string    `json:"vm_id"`
	AgentID          string    `json:"agent_id"`
	AgentOperationID string    `json:"agent_operation_id,omitempty"`
	Type             string    `json:"type"`
	Status           string    `json:"status"`
	Error            string    `json:"error,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Event struct {
	Type      string     `json:"type"`
	AgentID   string     `json:"agent_id,omitempty"`
	VMID      string     `json:"vm_id,omitempty"`
	Operation *Operation `json:"operation,omitempty"`
	VM        *VM        `json:"vm,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
}

type NetworkAllocation struct {
	GuestIP   string `json:"guest_ip"`
	GuestMAC  string `json:"guest_mac"`
	GatewayIP string `json:"gateway_ip"`
	Netmask   string `json:"netmask"`
}
