export type VMState =
  "creating" | "stopped" | "running" | "error" | "unknown" | "deleted" | string;

export interface VM {
  id: string;
  name: string;
  agent_id: string;
  state: VMState;
  vcpu_count: number;
  memory_mib: number;
  kernel_path: string;
  rootfs_path: string;
  disk_path?: string;
  tap_device?: string;
  guest_ip: string;
  guest_mac: string;
  gateway_ip: string;
  netmask: string;
  created_at: string;
  updated_at: string;
}

export interface Agent {
  id: string;
  hostname: string;
  url: string;
  status: string;
  vcpu_total: number;
  vcpu_available: number;
  disk_total_bytes: number;
  disk_free_bytes: number;
  last_seen: string;
}

export interface Operation {
  id: string;
  vm_id: string;
  agent_id: string;
  agent_operation_id?: string;
  type: "create" | "start" | "stop" | "reboot" | "delete" | string;
  status: "queued" | "running" | "succeeded" | "failed" | string;
  error?: string;
  created_at?: string;
  updated_at?: string;
}

export interface OperationAccepted {
  operation_id: string;
  vm_id: string;
  status: string;
}

export interface ManagerEvent {
  type: string;
  agent_id?: string;
  vm_id?: string;
  operation?: Partial<Operation>;
  vm?: VM;
  timestamp?: string;
}

export interface CreateVMRequest {
  name: string;
  agent_id?: string;
  vcpu_count: number;
  memory_mib: number;
  kernel_path: string;
  rootfs_path: string;
  disk_path?: string;
  tap_device?: string;
}

export type VMAction = "start" | "stop" | "reboot" | "delete";
