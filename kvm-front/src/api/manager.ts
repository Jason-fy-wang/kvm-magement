import type {
  Agent,
  CreateVMRequest,
  ManagerEvent,
  Operation,
  OperationAccepted,
  VM,
  VMAction,
} from "./types";

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const headers = new Headers(options?.headers);
  if (options?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(`/api/v1${path}`, {
    ...options,
    headers,
  });

  if (!response.ok) {
    const message = (await response.text()).trim();
    throw new Error(message || `${response.status} ${response.statusText}`);
  }

  return (await response.json()) as T;
}

function list<T>(data: T[] | null): T[] {
  return data ?? [];
}

export const managerApi = {
  async listVMs(): Promise<VM[]> {
    return list(await request<VM[] | null>("/vms"));
  },

  async listAgents(): Promise<Agent[]> {
    return list(await request<Agent[] | null>("/agents"));
  },

  createVM(input: CreateVMRequest): Promise<OperationAccepted> {
    return request("/vms", { method: "POST", body: JSON.stringify(input) });
  },

  getVM(id: string): Promise<VM> {
    return request(`/vms/${encodeURIComponent(id)}`);
  },

  runVMAction(id: string, action: VMAction): Promise<OperationAccepted> {
    const path = `/vms/${encodeURIComponent(id)}${action === "delete" ? "" : `/${action}`}`;
    return request(path, { method: action === "delete" ? "DELETE" : "POST" });
  },

  getOperation(id: string): Promise<Operation> {
    return request(`/operations/${encodeURIComponent(id)}`);
  },

  eventsURL(): string {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    return `${protocol}//${window.location.host}/api/v1/events`;
  },

  consoleURL(id: string): string {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    return `${protocol}//${window.location.host}/api/v1/vms/${encodeURIComponent(id)}/console`;
  },
};

export function parseManagerEvent(data: string): ManagerEvent {
  return JSON.parse(data) as ManagerEvent;
}
