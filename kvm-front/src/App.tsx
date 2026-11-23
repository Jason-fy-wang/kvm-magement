import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import {
  Activity,
  AlertCircle,
  ArrowUpRight,
  Check,
  ChevronDown,
  CircleDot,
  Cpu,
  HardDrive,
  LayoutDashboard,
  LoaderCircle,
  Menu,
  Monitor,
  Network,
  Plus,
  Power,
  RefreshCw,
  Search,
  Server,
  Settings2,
  TerminalSquare,
  Trash2,
  X,
  Zap,
} from "lucide-react";

type VMState =
  "creating" | "stopped" | "running" | "stopping" | "error" | string;

type VM = {
  id: string;
  name: string;
  agent_id: string;
  state: VMState;
  vcpu_count: number;
  memory_mib: number;
  guest_ip?: string;
  guest_mac?: string;
  gateway_ip?: string;
  netmask?: string;
  kernel_path: string;
  rootfs_path: string;
  tap_device?: string;
  created_at: string;
  updated_at: string;
};

type Agent = {
  id: string;
  hostname: string;
  url: string;
  status: string;
  vcpu_total: number;
  vcpu_available: number;
  disk_total_bytes: number;
  disk_free_bytes: number;
  last_seen: string;
};

type Operation = {
  id: string;
  vm_id: string;
  type: string;
  status: string;
  error?: string;
};

type CreateForm = {
  name: string;
  agent_id: string;
  vcpu_count: number;
  memory_mib: number;
  kernel_path: string;
  rootfs_path: string;
  disk_path: string;
  tap_device: string;
};

const emptyForm: CreateForm = {
  name: "",
  agent_id: "",
  vcpu_count: 2,
  memory_mib: 256,
  kernel_path: "/home/scott/firecracker/vmlinux-6.18.44",
  rootfs_path: "/home/scott/firecracker/ubuntu-24.04.ext4",
  disk_path: "",
  tap_device: "tap0",
};

async function api<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: {
      "Content-Type": "application/json",
      ...(options?.headers ?? {}),
    },
    ...options,
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || `${response.status} ${response.statusText}`);
  }
  return response.json() as Promise<T>;
}

function formatBytes(bytes: number) {
  if (!bytes) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const power = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1,
  );
  return `${(bytes / 1024 ** power).toFixed(power > 1 ? 1 : 0)} ${units[power]}`;
}

function formatTime(value?: string) {
  if (!value) return "—";
  return new Intl.DateTimeFormat("en", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(value));
}

function stateTone(state: VMState) {
  if (state === "running") return "success";
  if (state === "error") return "danger";
  if (state === "creating" || state === "stopping") return "warning";
  return "muted";
}

function StatusPill({ state }: { state: string }) {
  const tone = stateTone(state);
  return (
    <span className={`status-pill ${tone}`}>
      <span className="status-dot" />
      {state}
    </span>
  );
}

function App() {
  const [vms, setVms] = useState<VM[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [stateFilter, setStateFilter] = useState("all");
  const [createOpen, setCreateOpen] = useState(false);
  const [consoleVM, setConsoleVM] = useState<VM | null>(null);
  const [selectedVM, setSelectedVM] = useState<VM | null>(null);
  const [form, setForm] = useState<CreateForm>(emptyForm);
  const [submitting, setSubmitting] = useState(false);
  const [toast, setToast] = useState<{
    tone: "success" | "danger";
    message: string;
  } | null>(null);

  const loadData = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    setRefreshing(true);
    try {
      const [vmData, agentData] = await Promise.all([
        api<VM[]>("/api/v1/vms"),
        api<Agent[]>("/api/v1/agents"),
      ]);
      setVms(vmData);
      setAgents(agentData);
      setError("");
    } catch (loadError) {
      setError(
        loadError instanceof Error
          ? loadError.message
          : "Manager is unavailable",
      );
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void loadData();
    const eventURL = `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}/api/v1/events`;
    const socket = new WebSocket(eventURL);
    socket.onmessage = (message) => {
      try {
        const event = JSON.parse(message.data) as {
          type: string;
          vm?: VM;
          operation?: Operation;
        };
        const eventVM = event.vm;
        if (eventVM) {
          setVms((current) =>
            current.map((vm) =>
              vm.id === eventVM.id ? { ...vm, ...eventVM } : vm,
            ),
          );
          setSelectedVM((current) =>
            current?.id === eventVM.id ? { ...current, ...eventVM } : current,
          );
        }
        if (event.operation?.status === "succeeded") {
          void loadData(true);
        }
      } catch {
        // Ignore malformed event frames; the next refresh restores the state.
      }
    };
    return () => socket.close();
  }, [loadData]);

  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), 4200);
    return () => window.clearTimeout(timer);
  }, [toast]);

  const filteredVMs = useMemo(() => {
    const query = search.toLowerCase().trim();
    return vms.filter((vm) => {
      const matchesState = stateFilter === "all" || vm.state === stateFilter;
      const matchesSearch =
        !query ||
        [vm.name, vm.id, vm.guest_ip, vm.agent_id].some((value) =>
          value?.toLowerCase().includes(query),
        );
      return matchesState && matchesSearch;
    });
  }, [search, stateFilter, vms]);

  const running = vms.filter((vm) => vm.state === "running").length;
  const stopped = vms.filter((vm) => vm.state === "stopped").length;
  const onlineAgents = agents.filter(
    (agent) => agent.status === "online",
  ).length;
  const allocatedCpu = vms.reduce((total, vm) => total + vm.vcpu_count, 0);
  const cpuCapacity = agents.reduce(
    (total, agent) => total + agent.vcpu_total,
    0,
  );

  async function runAction(
    vm: VM,
    action: "start" | "stop" | "reboot" | "delete",
  ) {
    try {
      const method = action === "delete" ? "DELETE" : "POST";
      await api<{ operation_id: string }>(
        `/api/v1/vms/${vm.id}${action === "delete" ? "" : `/${action}`}`,
        { method },
      );
      setToast({
        tone: "success",
        message: `${action[0].toUpperCase() + action.slice(1)} requested for ${vm.name}`,
      });
      if (action === "delete") setSelectedVM(null);
      await loadData(true);
    } catch (actionError) {
      setToast({
        tone: "danger",
        message:
          actionError instanceof Error ? actionError.message : "Action failed",
      });
    }
  }

  async function createVM(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    try {
      await api("/api/v1/vms", {
        method: "POST",
        body: JSON.stringify({
          ...form,
          vcpu_count: Number(form.vcpu_count),
          memory_mib: Number(form.memory_mib),
        }),
      });
      setCreateOpen(false);
      setForm(emptyForm);
      setToast({ tone: "success", message: "VM creation queued" });
      await loadData(true);
    } catch (createError) {
      setToast({
        tone: "danger",
        message:
          createError instanceof Error
            ? createError.message
            : "Unable to create VM",
      });
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand-row">
          <div className="brand-mark">
            <Zap size={17} strokeWidth={2.5} />
          </div>
          <div>
            <div className="brand-name">KVM Control</div>
            <div className="brand-caption">microVM operations</div>
          </div>
        </div>
        <nav className="nav-list">
          <div className="nav-label">Workspace</div>
          <button className="nav-item active">
            <LayoutDashboard size={16} />
            Overview<span className="nav-count">{vms.length}</span>
          </button>
          <button
            className="nav-item"
            onClick={() => setStateFilter("running")}
          >
            <Activity size={16} />
            Running VMs<span className="nav-count">{running}</span>
          </button>
          <button
            className="nav-item"
            onClick={() => setStateFilter("stopped")}
          >
            <Power size={16} />
            Stopped VMs<span className="nav-count">{stopped}</span>
          </button>
          <div className="nav-label second">Infrastructure</div>
          <button className="nav-item">
            <Server size={16} />
            Physical servers<span className="nav-count">{onlineAgents}</span>
          </button>
          <button className="nav-item">
            <Network size={16} />
            Networks
          </button>
          <button className="nav-item">
            <Settings2 size={16} />
            Settings
          </button>
        </nav>
        <div className="sidebar-bottom">
          <div className="connection-card">
            <span className="online-pulse" />
            Manager connected
            <div className="connection-url">localhost:8080</div>
          </div>
          <div className="user-row">
            <div className="avatar">SC</div>
            <div>
              <div className="user-name">Scott Chen</div>
              <div className="user-role">administrator</div>
            </div>
            <ChevronDown size={14} className="muted-icon" />
          </div>
        </div>
      </aside>

      <main className="main-content">
        <header className="topbar">
          <div className="mobile-brand">
            <button className="icon-button">
              <Menu size={18} />
            </button>
            <span>KVM Control</span>
          </div>
          <div className="breadcrumb">
            <span>Workspace</span>
            <span className="slash">/</span>
            <strong>Overview</strong>
          </div>
          <div className="topbar-actions">
            <span className="live-indicator">
              <span className="live-dot" />
              Live updates
            </span>
            <button
              className="icon-button"
              title="Refresh"
              onClick={() => void loadData()}
            >
              <RefreshCw size={16} className={refreshing ? "spin" : ""} />
            </button>
            <div className="top-avatar">SC</div>
          </div>
        </header>

        <div className="content-wrap">
          <section className="page-intro">
            <div>
              <div className="eyebrow">Infrastructure / Control plane</div>
              <h1>VM overview</h1>
              <p>Manage Firecracker microVMs across your physical hosts.</p>
            </div>
            <button
              className="primary-button"
              onClick={() => setCreateOpen(true)}
            >
              <Plus size={17} />
              Create VM
            </button>
          </section>

          <section className="metrics-grid">
            <MetricCard
              label="Total VMs"
              value={vms.length.toString()}
              detail={`${running} running now`}
              icon={<Monitor size={18} />}
              tone="blue"
            />
            <MetricCard
              label="Running"
              value={running.toString()}
              detail={
                vms.length
                  ? `${Math.round((running / vms.length) * 100)}% of inventory`
                  : "No workloads"
              }
              icon={<Activity size={18} />}
              tone="cyan"
            />
            <MetricCard
              label="vCPU allocation"
              value={
                cpuCapacity
                  ? `${allocatedCpu}/${cpuCapacity}`
                  : `${allocatedCpu}`
              }
              detail="Across online hosts"
              icon={<Cpu size={18} />}
              tone="amber"
            />
            <MetricCard
              label="Online hosts"
              value={`${onlineAgents}/${agents.length}`}
              detail="Agent heartbeat status"
              icon={<Server size={18} />}
              tone="violet"
            />
          </section>

          <section className="workspace-grid">
            <div className="table-panel">
              <div className="panel-header">
                <div>
                  <h2>Virtual machines</h2>
                  <p>Every VM registered with the control plane</p>
                </div>
                <div className="panel-actions">
                  <div className="search-box">
                    <Search size={15} />
                    <input
                      value={search}
                      onChange={(event) => setSearch(event.target.value)}
                      placeholder="Search VMs"
                    />
                  </div>
                  <select
                    value={stateFilter}
                    onChange={(event) => setStateFilter(event.target.value)}
                  >
                    <option value="all">All states</option>
                    <option value="running">Running</option>
                    <option value="stopped">Stopped</option>
                    <option value="creating">Creating</option>
                    <option value="error">Error</option>
                  </select>
                </div>
              </div>
              {error && (
                <div className="error-banner">
                  <AlertCircle size={16} />
                  <span>{error}</span>
                  <button onClick={() => void loadData()}>Retry</button>
                </div>
              )}
              <div className="table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>VM</th>
                      <th>State</th>
                      <th>Resources</th>
                      <th>Network</th>
                      <th>Host</th>
                      <th>Updated</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {loading ? (
                      <tr>
                        <td colSpan={7}>
                          <div className="loading-state">
                            <LoaderCircle size={20} className="spin" />
                            Loading inventory
                          </div>
                        </td>
                      </tr>
                    ) : filteredVMs.length === 0 ? (
                      <tr>
                        <td colSpan={7}>
                          <div className="empty-state">
                            <Monitor size={24} />
                            <strong>
                              {vms.length
                                ? "No matching VMs"
                                : "No VMs registered"}
                            </strong>
                            <span>
                              {vms.length
                                ? "Try a different filter or search term."
                                : "Create your first microVM to get started."}
                            </span>
                          </div>
                        </td>
                      </tr>
                    ) : (
                      filteredVMs.map((vm) => (
                        <VMRow
                          key={vm.id}
                          vm={vm}
                          onSelect={setSelectedVM}
                          onConsole={setConsoleVM}
                          onAction={runAction}
                        />
                      ))
                    )}
                  </tbody>
                </table>
              </div>
              <div className="table-footer">
                <span>
                  Showing {filteredVMs.length} of {vms.length} VMs
                </span>
                <span className="footer-status">
                  <span className="status-dot success-dot" />
                  Live data
                </span>
              </div>
            </div>
            <aside className="side-stack">
              <div className="side-panel">
                <div className="side-panel-header">
                  <div>
                    <h2>Physical hosts</h2>
                    <p>Agent heartbeat overview</p>
                  </div>
                  <ArrowUpRight size={16} className="muted-icon" />
                </div>
                <div className="agent-list">
                  {agents.length === 0 ? (
                    <div className="side-empty">No agents connected</div>
                  ) : (
                    agents.map((agent) => (
                      <AgentRow key={agent.id} agent={agent} />
                    ))
                  )}
                </div>
              </div>
              <div className="side-panel capacity-panel">
                <div className="side-panel-header">
                  <div>
                    <h2>Capacity</h2>
                    <p>Current allocation</p>
                  </div>
                  <HardDrive size={17} className="muted-icon" />
                </div>
                <CapacityBar
                  label="vCPU"
                  used={allocatedCpu}
                  total={cpuCapacity || Math.max(allocatedCpu, 1)}
                  color="cyan"
                />
                <CapacityBar
                  label="Memory"
                  used={vms.reduce((sum, vm) => sum + vm.memory_mib, 0)}
                  total={
                    agents.length
                      ? agents.reduce(
                          (sum, agent) => sum + agent.vcpu_total * 512,
                          0,
                        )
                      : Math.max(
                          vms.reduce((sum, vm) => sum + vm.memory_mib, 0),
                          1,
                        )
                  }
                  color="sky"
                />
              </div>
            </aside>
          </section>
        </div>
      </main>

      {createOpen && (
        <CreateDrawer
          form={form}
          agents={agents}
          submitting={submitting}
          onChange={setForm}
          onClose={() => setCreateOpen(false)}
          onSubmit={createVM}
        />
      )}
      {selectedVM && (
        <VMDrawer
          vm={selectedVM}
          onClose={() => setSelectedVM(null)}
          onAction={runAction}
          onConsole={setConsoleVM}
        />
      )}
      {consoleVM && (
        <ConsoleDrawer vm={consoleVM} onClose={() => setConsoleVM(null)} />
      )}
      {toast && (
        <div className={`toast ${toast.tone}`}>
          <span className="toast-icon">
            {toast.tone === "success" ? (
              <Check size={15} />
            ) : (
              <AlertCircle size={15} />
            )}
          </span>
          {toast.message}
          <button onClick={() => setToast(null)}>
            <X size={14} />
          </button>
        </div>
      )}
    </div>
  );
}

function MetricCard({
  label,
  value,
  detail,
  icon,
  tone,
}: {
  label: string;
  value: string;
  detail: string;
  icon: React.ReactNode;
  tone: string;
}) {
  return (
    <div className="metric-card">
      <div className={`metric-icon ${tone}`}>{icon}</div>
      <div className="metric-copy">
        <span>{label}</span>
        <strong>{value}</strong>
        <small>{detail}</small>
      </div>
    </div>
  );
}

function VMRow({
  vm,
  onSelect,
  onConsole,
  onAction,
}: {
  vm: VM;
  onSelect: (vm: VM) => void;
  onConsole: (vm: VM) => void;
  onAction: (vm: VM, action: "start" | "stop" | "reboot" | "delete") => void;
}) {
  return (
    <tr className="vm-row" onClick={() => onSelect(vm)}>
      <td>
        <div className="vm-name-cell">
          <div className="vm-icon">
            <Monitor size={16} />
          </div>
          <div>
            <strong>{vm.name || "Unnamed VM"}</strong>
            <span>{vm.id}</span>
          </div>
        </div>
      </td>
      <td>
        <StatusPill state={vm.state} />
      </td>
      <td>
        <div className="resource-cell">
          <span>{vm.vcpu_count} vCPU</span>
          <span>{vm.memory_mib} MiB</span>
        </div>
      </td>
      <td>
        <div className="network-cell">
          <span>{vm.guest_ip || "Unassigned"}</span>
          <span>{vm.guest_mac || "—"}</span>
        </div>
      </td>
      <td>
        <span className="host-cell">
          <CircleDot size={13} />
          {vm.agent_id || "Unassigned"}
        </span>
      </td>
      <td>
        <span className="updated-cell">{formatTime(vm.updated_at)}</span>
      </td>
      <td>
        <div
          className="row-actions"
          onClick={(event) => event.stopPropagation()}
        >
          <button title="Open console" onClick={() => onConsole(vm)}>
            <TerminalSquare size={15} />
          </button>
          <button
            title={vm.state === "running" ? "Stop VM" : "Start VM"}
            onClick={() =>
              onAction(vm, vm.state === "running" ? "stop" : "start")
            }
          >
            <Power size={15} />
          </button>
          <button title="Delete VM" onClick={() => onAction(vm, "delete")}>
            <Trash2 size={15} />
          </button>
        </div>
      </td>
    </tr>
  );
}

function AgentRow({ agent }: { agent: Agent }) {
  const percentage = agent.vcpu_total
    ? Math.round(
        ((agent.vcpu_total - agent.vcpu_available) / agent.vcpu_total) * 100,
      )
    : 0;
  return (
    <div className="agent-row">
      <div className="agent-avatar">
        <Server size={15} />
      </div>
      <div className="agent-copy">
        <strong>{agent.hostname || agent.id}</strong>
        <span>
          {agent.id} · {agent.vcpu_available || 0} vCPU free
        </span>
        <div className="mini-bar">
          <span style={{ width: `${Math.max(percentage, 4)}%` }} />
        </div>
      </div>
      <StatusPill state={agent.status} />
    </div>
  );
}

function CapacityBar({
  label,
  used,
  total,
  color,
}: {
  label: string;
  used: number;
  total: number;
  color: string;
}) {
  const percentage = Math.min(
    Math.round((used / Math.max(total, 1)) * 100),
    100,
  );
  return (
    <div className="capacity-row">
      <div>
        <span>{label}</span>
        <strong>
          {used.toLocaleString()} <small>/ {total.toLocaleString()}</small>
        </strong>
      </div>
      <div className="capacity-track">
        <span className={color} style={{ width: `${percentage}%` }} />
      </div>
    </div>
  );
}

function Field({
  label,
  children,
  hint,
}: {
  label: string;
  children: React.ReactNode;
  hint?: string;
}) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
      {hint && <small>{hint}</small>}
    </label>
  );
}

function CreateDrawer({
  form,
  agents,
  submitting,
  onChange,
  onClose,
  onSubmit,
}: {
  form: CreateForm;
  agents: Agent[];
  submitting: boolean;
  onChange: (form: CreateForm) => void;
  onClose: () => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const update = <K extends keyof CreateForm>(key: K, value: CreateForm[K]) =>
    onChange({ ...form, [key]: value });
  return (
    <div
      className="drawer-backdrop"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <section className="drawer create-drawer">
        <div className="drawer-header">
          <div>
            <div className="eyebrow">Provisioning</div>
            <h2>Create virtual machine</h2>
          </div>
          <button className="close-button" onClick={onClose}>
            <X size={18} />
          </button>
        </div>
        <form onSubmit={onSubmit} className="drawer-form">
          <div className="form-section">
            <div className="form-section-title">Identity</div>
            <div className="field-grid">
              <Field label="VM name">
                <input
                  required
                  value={form.name}
                  onChange={(event) => update("name", event.target.value)}
                  placeholder="e.g. api-worker-01"
                />
              </Field>
              <Field
                label="Physical host"
                hint="Leave automatic to let the Manager select a host."
              >
                <select
                  value={form.agent_id}
                  onChange={(event) => update("agent_id", event.target.value)}
                >
                  <option value="">Automatic placement</option>
                  {agents.map((agent) => (
                    <option key={agent.id} value={agent.id}>
                      {agent.hostname || agent.id} · {agent.status}
                    </option>
                  ))}
                </select>
              </Field>
            </div>
          </div>
          <div className="form-section">
            <div className="form-section-title">Compute</div>
            <div className="field-grid">
              <Field label="vCPU">
                <input
                  required
                  min="1"
                  type="number"
                  value={form.vcpu_count}
                  onChange={(event) =>
                    update("vcpu_count", Number(event.target.value))
                  }
                />
              </Field>
              <Field label="Memory (MiB)">
                <input
                  required
                  min="128"
                  step="128"
                  type="number"
                  value={form.memory_mib}
                  onChange={(event) =>
                    update("memory_mib", Number(event.target.value))
                  }
                />
              </Field>
            </div>
          </div>
          <div className="form-section">
            <div className="form-section-title">Boot media</div>
            <Field label="Kernel image path">
              <input
                required
                value={form.kernel_path}
                onChange={(event) => update("kernel_path", event.target.value)}
              />
            </Field>
            <Field label="Root filesystem path">
              <input
                required
                value={form.rootfs_path}
                onChange={(event) => update("rootfs_path", event.target.value)}
              />
            </Field>
            <Field
              label="Additional disk path"
              hint="Optional. Files are retained after VM deletion."
            >
              <input
                value={form.disk_path}
                onChange={(event) => update("disk_path", event.target.value)}
                placeholder="/var/lib/kvm/images/data.ext4"
              />
            </Field>
          </div>
          <div className="form-section">
            <div className="form-section-title">Network</div>
            <Field label="TAP device">
              <input
                value={form.tap_device}
                onChange={(event) => update("tap_device", event.target.value)}
                placeholder="tap-vm-name"
              />
            </Field>
            <div className="network-note">
              <Network size={15} />
              <span>
                The Manager will allocate the guest IP and derive its MAC from
                the address.
              </span>
            </div>
          </div>
          <div className="drawer-footer">
            <button
              type="button"
              className="secondary-button"
              onClick={onClose}
            >
              Cancel
            </button>
            <button
              disabled={submitting}
              className="primary-button"
              type="submit"
            >
              {submitting ? (
                <>
                  <LoaderCircle size={16} className="spin" />
                  Creating
                </>
              ) : (
                <>
                  <Plus size={16} />
                  Create VM
                </>
              )}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}

function VMDrawer({
  vm,
  onClose,
  onAction,
  onConsole,
}: {
  vm: VM;
  onClose: () => void;
  onAction: (vm: VM, action: "start" | "stop" | "reboot" | "delete") => void;
  onConsole: (vm: VM) => void;
}) {
  return (
    <div
      className="drawer-backdrop"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <section className="drawer detail-drawer">
        <div className="drawer-header">
          <div>
            <div className="eyebrow">Virtual machine</div>
            <h2>{vm.name || "Unnamed VM"}</h2>
            <span className="drawer-id">{vm.id}</span>
          </div>
          <button className="close-button" onClick={onClose}>
            <X size={18} />
          </button>
        </div>
        <div className="detail-state">
          <StatusPill state={vm.state} />
          <span>Updated {formatTime(vm.updated_at)}</span>
        </div>
        <div className="detail-actions">
          <button className="primary-button" onClick={() => onConsole(vm)}>
            <TerminalSquare size={16} />
            Open console
          </button>
          <button
            className="secondary-button"
            onClick={() =>
              onAction(vm, vm.state === "running" ? "stop" : "start")
            }
          >
            <Power size={16} />
            {vm.state === "running" ? "Stop" : "Start"}
          </button>
          <button
            className="danger-button"
            onClick={() => onAction(vm, "delete")}
          >
            <Trash2 size={16} />
            Delete
          </button>
        </div>
        <div className="detail-section">
          <div className="form-section-title">Configuration</div>
          <DetailLine label="Host" value={vm.agent_id} />
          <DetailLine label="vCPU" value={`${vm.vcpu_count}`} />
          <DetailLine label="Memory" value={`${vm.memory_mib} MiB`} />
          <DetailLine label="TAP" value={vm.tap_device || "—"} />
        </div>
        <div className="detail-section">
          <div className="form-section-title">Network</div>
          <DetailLine label="Guest IP" value={vm.guest_ip || "—"} mono />
          <DetailLine label="MAC address" value={vm.guest_mac || "—"} mono />
          <DetailLine label="Gateway" value={vm.gateway_ip || "—"} mono />
          <DetailLine label="Netmask" value={vm.netmask || "—"} mono />
        </div>
        <div className="detail-section">
          <div className="form-section-title">Storage</div>
          <DetailLine label="Kernel" value={vm.kernel_path} mono />
          <DetailLine label="Root filesystem" value={vm.rootfs_path} mono />
        </div>
      </section>
    </div>
  );
}

function DetailLine({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="detail-line">
      <span>{label}</span>
      <strong className={mono ? "mono" : ""}>{value}</strong>
    </div>
  );
}

function ConsoleDrawer({ vm, onClose }: { vm: VM; onClose: () => void }) {
  const [connected, setConnected] = useState(false);
  const [lines, setLines] = useState<string[]>([
    "Connecting to serial console…",
  ]);
  useEffect(() => {
    const url = `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}/api/v1/vms/${vm.id}/console`;
    const socket = new WebSocket(url);
    socket.onopen = () => {
      setConnected(true);
      setLines((current) => [...current, "Connected."]);
    };
    socket.onmessage = (event) =>
      setLines((current) => [...current, String(event.data)]);
    socket.onerror = () =>
      setLines((current) => [...current, "Console connection failed."]);
    socket.onclose = () => setConnected(false);
    return () => socket.close();
  }, [vm.id]);
  return (
    <div
      className="drawer-backdrop"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <section className="drawer console-drawer">
        <div className="drawer-header">
          <div>
            <div className="eyebrow">Serial console</div>
            <h2>{vm.name}</h2>
          </div>
          <div className="console-heading-actions">
            <span className={`console-status ${connected ? "connected" : ""}`}>
              <span />
              {connected ? "Connected" : "Offline"}
            </span>
            <button className="close-button" onClick={onClose}>
              <X size={18} />
            </button>
          </div>
        </div>
        <div className="terminal-window">
          <div className="terminal-top">
            <span />
            <span />
            <span />
            <small>{vm.id} / ttyS0</small>
          </div>
          <div className="terminal-body">
            {lines.map((line, index) => (
              <div key={`${index}-${line}`} className="terminal-line">
                {line}
              </div>
            ))}
            <span className="cursor" />
          </div>
        </div>
        <div className="console-footer">
          <span>
            <CircleDot size={12} />
            Firecracker serial stream
          </span>
          <span>Use the guest login configured in the rootfs.</span>
        </div>
      </section>
    </div>
  );
}

export default App;
