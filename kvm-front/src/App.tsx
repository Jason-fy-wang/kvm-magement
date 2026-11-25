import { useCallback, useEffect, useRef, useState } from "react";
import {
  Activity,
  AlertCircle,
  Check,
  ChevronDown,
  Cpu,
  LayoutDashboard,
  LoaderCircle,
  Menu,
  Monitor,
  Plus,
  Power,
  RefreshCw,
  Server,
  Zap,
} from "lucide-react";
import { managerApi, parseManagerEvent } from "./api/manager";
import type {
  Agent,
  CreateVMRequest,
  Operation,
  VM,
  VMAction,
} from "./api/types";
import { AgentPanel } from "./component/AgentPanel";
import { ConsoleDrawer } from "./component/ConsoleDrawer";
import { CreateVMDialog } from "./component/CreateVMDialog";
import { DeleteVMDialog } from "./component/DeleteVMDialog";
import { MetricCard } from "./component/MetricCard";
import { StatusBadge } from "./component/StatusBadge";
import { Toast, type ToastMessage } from "./component/Toast";
import { VMDetailDrawer } from "./component/VMDetailDrawer";
import { VMTable } from "./component/VMTable";
import { formatTime } from "./utils/format";

type View = "overview" | "vms" | "agents" | "operations";
type TrackedOperation = {
  operationId: string;
  vmId: string;
  vmName: string;
  action: string;
  status: string;
  error?: string;
  updatedAt: string;
  startAfterCreate?: boolean;
};

function isTerminal(status: string) {
  return status === "succeeded" || status === "failed";
}

function App() {
  const [vms, setVms] = useState<VM[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [operations, setOperations] = useState<TrackedOperation[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [live, setLive] = useState(false);
  const [error, setError] = useState("");
  const [view, setView] = useState<View>("overview");
  const [vmStateFilter, setVmStateFilter] = useState("all");
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [selectedVM, setSelectedVM] = useState<VM | null>(null);
  const [consoleVM, setConsoleVM] = useState<VM | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<VM | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [toast, setToast] = useState<ToastMessage | null>(null);
  const inFlight = useRef(new Set<string>());
  const chainedCreates = useRef(new Set<string>());

  const loadData = useCallback(async (showSpinner = false) => {
    if (showSpinner) setRefreshing(true);
    try {
      const [vmData, agentData] = await Promise.all([
        managerApi.listVMs(),
        managerApi.listAgents(),
      ]);
      const activeVMs = vmData.filter((vm) => vm.state !== "deleted");
      setVms(activeVMs);
      setSelectedVM((current) =>
        current ? (activeVMs.find((vm) => vm.id === current.id) ?? null) : null,
      );
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

  const addOperation = useCallback((operation: TrackedOperation) => {
    setOperations((current) => [
      operation,
      ...current.filter((item) => item.operationId !== operation.operationId),
    ]);
  }, []);

  useEffect(() => {
    void loadData();
    let socket: WebSocket | undefined;
    let reconnectTimer = 0;
    let reconnectDelay = 500;
    let disposed = false;

    const connect = () => {
      if (disposed) return;
      const connection = new WebSocket(managerApi.eventsURL());
      socket = connection;
      connection.onopen = () => {
        reconnectDelay = 500;
        setLive(true);
        void loadData();
      };
      connection.onmessage = (message) => {
        try {
          const event = parseManagerEvent(String(message.data));
          if (event.vm) {
            const eventVM = event.vm;
            setVms((current) => {
              if (eventVM.state === "deleted")
                return current.filter((vm) => vm.id !== eventVM.id);
              const found = current.some((vm) => vm.id === eventVM.id);
              return found
                ? current.map((vm) =>
                    vm.id === eventVM.id ? { ...vm, ...eventVM } : vm,
                  )
                : [eventVM, ...current];
            });
            setSelectedVM((current) =>
              current?.id === eventVM.id ? { ...current, ...eventVM } : current,
            );
          }
          if (event.operation && isTerminal(event.operation.status ?? "")) {
            void loadData();
          }
        } catch {
          setToast({
            tone: "danger",
            message: "Received an invalid live event from the manager.",
          });
        }
      };
      connection.onclose = () => {
        setLive(false);
        if (disposed) return;
        reconnectTimer = window.setTimeout(connect, reconnectDelay);
        reconnectDelay = Math.min(reconnectDelay * 2, 15_000);
      };
      connection.onerror = () => connection.close();
    };

    connect();
    return () => {
      disposed = true;
      window.clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, [loadData]);

  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), 6000);
    return () => window.clearTimeout(timer);
  }, [toast]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      for (const tracked of operations) {
        if (
          isTerminal(tracked.status) ||
          inFlight.current.has(tracked.operationId)
        )
          continue;
        inFlight.current.add(tracked.operationId);
        void managerApi
          .getOperation(tracked.operationId)
          .then(async (operation: Operation) => {
            setOperations((current) =>
              current.map((item) =>
                item.operationId === tracked.operationId
                  ? {
                      ...item,
                      status: operation.status,
                      error: operation.error,
                      updatedAt:
                        operation.updated_at || new Date().toISOString(),
                    }
                  : item,
              ),
            );
            if (!isTerminal(operation.status)) return;

            await loadData();
            if (operation.status === "failed") {
              setToast({
                tone: "danger",
                message: `${tracked.action} failed for ${tracked.vmName}: ${operation.error || "No error details were provided."}`,
              });
              return;
            }

            if (tracked.action === "delete") {
              setSelectedVM((current) =>
                current?.id === tracked.vmId ? null : current,
              );
            }
            setToast({
              tone: "success",
              message: `${tracked.action} succeeded for ${tracked.vmName}.`,
            });

            if (
              tracked.action === "create" &&
              tracked.startAfterCreate &&
              !chainedCreates.current.has(tracked.operationId)
            ) {
              chainedCreates.current.add(tracked.operationId);
              try {
                const accepted = await managerApi.runVMAction(
                  tracked.vmId,
                  "start",
                );
                addOperation({
                  operationId: accepted.operation_id,
                  vmId: tracked.vmId,
                  vmName: tracked.vmName,
                  action: "start",
                  status: accepted.status,
                  updatedAt: new Date().toISOString(),
                });
                setToast({
                  tone: "info",
                  message: `Creation succeeded. Start requested for ${tracked.vmName}.`,
                });
              } catch (startError) {
                setToast({
                  tone: "danger",
                  message:
                    startError instanceof Error
                      ? startError.message
                      : "VM creation succeeded, but starting it failed.",
                });
              }
            }
          })
          .catch((pollError: unknown) => {
            setToast({
              tone: "danger",
              message:
                pollError instanceof Error
                  ? `Could not check operation ${tracked.operationId}: ${pollError.message}`
                  : `Could not check operation ${tracked.operationId}.`,
            });
          })
          .finally(() => inFlight.current.delete(tracked.operationId));
      }
    }, 2000);
    return () => window.clearInterval(timer);
  }, [operations, addOperation, loadData]);

  async function createVM(input: CreateVMRequest, startAfterCreate: boolean) {
    setSubmitting(true);
    try {
      const accepted = await managerApi.createVM(input);
      addOperation({
        operationId: accepted.operation_id,
        vmId: accepted.vm_id,
        vmName: input.name,
        action: "create",
        status: accepted.status,
        updatedAt: new Date().toISOString(),
        startAfterCreate,
      });
      setCreateOpen(false);
      setToast({
        tone: "info",
        message: `Creation requested for ${input.name}.`,
      });
      await loadData();
    } catch (createError) {
      setToast({
        tone: "danger",
        message:
          createError instanceof Error
            ? createError.message
            : "Unable to create VM.",
      });
    } finally {
      setSubmitting(false);
    }
  }

  async function runAction(vm: VM, action: VMAction) {
    if (action === "delete") {
      setDeleteTarget(vm);
      return;
    }
    try {
      const accepted = await managerApi.runVMAction(vm.id, action);
      addOperation({
        operationId: accepted.operation_id,
        vmId: vm.id,
        vmName: vm.name,
        action,
        status: accepted.status,
        updatedAt: new Date().toISOString(),
      });
      setToast({
        tone: "info",
        message: `${action[0].toUpperCase()}${action.slice(1)} requested for ${vm.name}.`,
      });
      await loadData();
    } catch (actionError) {
      setToast({
        tone: "danger",
        message:
          actionError instanceof Error
            ? actionError.message
            : `Unable to ${action} ${vm.name}.`,
      });
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    const vm = deleteTarget;
    try {
      const accepted = await managerApi.runVMAction(vm.id, "delete");
      addOperation({
        operationId: accepted.operation_id,
        vmId: vm.id,
        vmName: vm.name,
        action: "delete",
        status: accepted.status,
        updatedAt: new Date().toISOString(),
      });
      setDeleteTarget(null);
      setToast({ tone: "info", message: `Deletion requested for ${vm.name}.` });
    } catch (deleteError) {
      setToast({
        tone: "danger",
        message:
          deleteError instanceof Error
            ? deleteError.message
            : `Unable to delete ${vm.name}.`,
      });
    }
  }

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
  const activeOperations = operations.filter(
    (operation) => !isTerminal(operation.status),
  );
  const pageTitles: Record<View, { title: string; subtitle: string }> = {
    overview: {
      title: "VM overview",
      subtitle: "Manage Firecracker microVMs across your physical hosts.",
    },
    vms: {
      title: "Virtual machines",
      subtitle: "Inspect VM configuration, status, and lifecycle actions.",
    },
    agents: {
      title: "Physical hosts",
      subtitle: "Monitor agent heartbeat and reported host capacity.",
    },
    operations: {
      title: "Operations",
      subtitle: "Track operations started during this browser session.",
    },
  };

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
        <nav className="nav-list" aria-label="Main navigation">
          <div className="nav-label">Workspace</div>
          <NavButton
            active={view === "overview"}
            onClick={() => setView("overview")}
            icon={<LayoutDashboard size={16} />}
            label="Overview"
            count={vms.length}
          />
          <NavButton
            active={view === "vms" && vmStateFilter === "all"}
            onClick={() => {
              setView("vms");
              setVmStateFilter("all");
            }}
            icon={<Monitor size={16} />}
            label="All VMs"
            count={vms.length}
          />
          <NavButton
            active={view === "vms" && vmStateFilter === "running"}
            onClick={() => {
              setView("vms");
              setVmStateFilter("running");
            }}
            icon={<Activity size={16} />}
            label="Running VMs"
            count={running}
          />
          <NavButton
            active={view === "vms" && vmStateFilter === "stopped"}
            onClick={() => {
              setView("vms");
              setVmStateFilter("stopped");
            }}
            icon={<Power size={16} />}
            label="Stopped VMs"
            count={stopped}
          />
          <div className="nav-label second">Infrastructure</div>
          <NavButton
            active={view === "agents"}
            onClick={() => setView("agents")}
            icon={<Server size={16} />}
            label="Physical hosts"
            count={onlineAgents}
          />
          <NavButton
            active={view === "operations"}
            onClick={() => setView("operations")}
            icon={<RefreshCw size={16} />}
            label="Operations"
            count={activeOperations.length}
          />
        </nav>
        <div className="sidebar-bottom">
          <div className="connection-card">
            <span className={live ? "online-pulse" : "offline-pulse"} />
            {live ? "Manager connected" : "Live updates disconnected"}
            <div className="connection-url">{window.location.host}</div>
          </div>
          <div className="user-row">
            <div className="avatar">KM</div>
            <div>
              <div className="user-name">KVM Manager</div>
              <div className="user-role">control plane</div>
            </div>
            <ChevronDown size={14} className="muted-icon" />
          </div>
        </div>
      </aside>

      <main className="main-content">
        <header className="topbar">
          <div className="mobile-brand">
            <button
              className="icon-button"
              aria-label={mobileMenuOpen ? "Close menu" : "Open menu"}
              aria-expanded={mobileMenuOpen}
              onClick={() => setMobileMenuOpen((open) => !open)}
            >
              <Menu size={18} />
            </button>
            <span>KVM Control</span>
          </div>
          <div className="breadcrumb">
            <span>Workspace</span>
            <span className="slash">/</span>
            <strong>{pageTitles[view].title}</strong>
          </div>
          <div className="topbar-actions">
            <span className={`live-indicator ${live ? "" : "disconnected"}`}>
              <span className={live ? "live-dot" : "offline-pulse"} />
              {live ? "Live updates" : "Reconnecting"}
            </span>
            <button
              className="icon-button"
              title="Refresh"
              aria-label="Refresh data"
              onClick={() => void loadData(true)}
            >
              <RefreshCw size={16} className={refreshing ? "spin" : ""} />
            </button>
          </div>
        </header>

        {mobileMenuOpen && (
          <nav className="mobile-nav" aria-label="Mobile navigation">
            <NavButton
              active={view === "overview"}
              onClick={() => {
                setView("overview");
                setMobileMenuOpen(false);
              }}
              icon={<LayoutDashboard size={16} />}
              label="Overview"
            />
            <NavButton
              active={view === "vms" && vmStateFilter === "all"}
              onClick={() => {
                setView("vms");
                setVmStateFilter("all");
                setMobileMenuOpen(false);
              }}
              icon={<Monitor size={16} />}
              label="All VMs"
              count={vms.length}
            />
            <NavButton
              active={view === "vms" && vmStateFilter === "running"}
              onClick={() => {
                setView("vms");
                setVmStateFilter("running");
                setMobileMenuOpen(false);
              }}
              icon={<Activity size={16} />}
              label="Running VMs"
              count={running}
            />
            <NavButton
              active={view === "vms" && vmStateFilter === "stopped"}
              onClick={() => {
                setView("vms");
                setVmStateFilter("stopped");
                setMobileMenuOpen(false);
              }}
              icon={<Power size={16} />}
              label="Stopped VMs"
              count={stopped}
            />
            <NavButton
              active={view === "agents"}
              onClick={() => {
                setView("agents");
                setMobileMenuOpen(false);
              }}
              icon={<Server size={16} />}
              label="Physical hosts"
              count={onlineAgents}
            />
            <NavButton
              active={view === "operations"}
              onClick={() => {
                setView("operations");
                setMobileMenuOpen(false);
              }}
              icon={<RefreshCw size={16} />}
              label="Operations"
              count={activeOperations.length}
            />
          </nav>
        )}

        {!live && (
          <div
            className="connection-banner flex items-center gap-2"
            role="status"
          >
            <AlertCircle size={16} />
            <span>
              Live updates disconnected. Reconnecting; the inventory will
              refresh when connected.
            </span>
          </div>
        )}
        <div className="content-wrap">
          <section className="page-intro">
            <div>
              <div className="eyebrow">Infrastructure / Control plane</div>
              <h1>{pageTitles[view].title}</h1>
              <p>{pageTitles[view].subtitle}</p>
            </div>
            {view !== "agents" && view !== "operations" && (
              <button
                className="primary-button"
                onClick={() => setCreateOpen(true)}
              >
                <Plus size={17} />
                Create VM
              </button>
            )}
          </section>

          {view === "overview" && (
            <>
              <section className="metrics-grid">
                <MetricCard
                  label="Total VMs"
                  value={String(vms.length)}
                  detail={`${running} running now`}
                  icon={<Monitor size={18} />}
                  tone="blue"
                />
                <MetricCard
                  label="Running"
                  value={String(running)}
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
                      : String(allocatedCpu)
                  }
                  detail="Across registered hosts"
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
              {activeOperations.length > 0 && (
                <OperationSummary operations={activeOperations} />
              )}
              <section className="workspace-grid">
                <VMTable
                  vms={vms}
                  loading={loading}
                  error={error}
                  stateFilter={vmStateFilter}
                  onRetry={() => void loadData(true)}
                  onStateFilterChange={setVmStateFilter}
                  onSelect={setSelectedVM}
                  onAction={(vm, action) => void runAction(vm, action)}
                  onConsole={setConsoleVM}
                />
                <aside className="side-stack">
                  <AgentPanel agents={agents} />
                  <CapacityPanel used={allocatedCpu} total={cpuCapacity} />
                </aside>
              </section>
            </>
          )}

          {view === "vms" && (
            <VMTable
              vms={vms}
              loading={loading}
              error={error}
              stateFilter={vmStateFilter}
              onRetry={() => void loadData(true)}
              onStateFilterChange={setVmStateFilter}
              onSelect={setSelectedVM}
              onAction={(vm, action) => void runAction(vm, action)}
              onConsole={setConsoleVM}
            />
          )}
          {view === "agents" && <AgentPanel agents={agents} />}
          {view === "operations" && (
            <OperationsTable operations={operations} vms={vms} />
          )}
        </div>
      </main>

      {createOpen && (
        <CreateVMDialog
          agents={agents}
          submitting={submitting}
          onClose={() => setCreateOpen(false)}
          onSubmit={createVM}
        />
      )}
      {selectedVM && (
        <VMDetailDrawer
          vm={selectedVM}
          onClose={() => setSelectedVM(null)}
          onAction={(vm, action) => void runAction(vm, action)}
          onConsole={setConsoleVM}
        />
      )}
      {consoleVM && (
        <ConsoleDrawer vm={consoleVM} onClose={() => setConsoleVM(null)} />
      )}
      {deleteTarget && (
        <DeleteVMDialog
          vm={deleteTarget}
          onClose={() => setDeleteTarget(null)}
          onConfirm={() => void confirmDelete()}
        />
      )}
      {toast && <Toast toast={toast} onDismiss={() => setToast(null)} />}
    </div>
  );
}

function NavButton({
  active,
  onClick,
  icon,
  label,
  count,
}: {
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  label: string;
  count?: number;
}) {
  return (
    <button className={`nav-item ${active ? "active" : ""}`} onClick={onClick}>
      {icon}
      {label}
      {count !== undefined && <span className="nav-count">{count}</span>}
    </button>
  );
}

function CapacityPanel({ used, total }: { used: number; total: number }) {
  const percentage = total
    ? Math.min(Math.round((used / total) * 100), 100)
    : 0;
  return (
    <div className="side-panel capacity-panel">
      <div className="side-panel-header">
        <div>
          <h2>Capacity</h2>
          <p>Reported vCPU allocation</p>
        </div>
        <Cpu size={17} className="muted-icon" />
      </div>
      <div className="capacity-row">
        <div>
          <span>vCPU</span>
          <strong>
            {used.toLocaleString()} <small>/ {total.toLocaleString()}</small>
          </strong>
        </div>
        <div className="capacity-track">
          <span className="cyan" style={{ width: `${percentage}%` }} />
        </div>
      </div>
      <p className="capacity-note">
        VM totals include all non-deleted manager records. The API does not
        report host memory capacity.
      </p>
    </div>
  );
}

function OperationSummary({ operations }: { operations: TrackedOperation[] }) {
  return (
    <section className="operation-summary" aria-label="Operations in progress">
      <div>
        <LoaderCircle size={16} className="spin" />
        <strong>
          {operations.length} operation{operations.length === 1 ? "" : "s"} in
          progress
        </strong>
      </div>
      {operations.slice(0, 3).map((operation) => (
        <span key={operation.operationId}>
          {operation.action} · {operation.vmName}
        </span>
      ))}
    </section>
  );
}

function OperationsTable({
  operations,
  vms,
}: {
  operations: TrackedOperation[];
  vms: VM[];
}) {
  const names = new Map(vms.map((vm) => [vm.id, vm.name]));
  return (
    <section className="table-panel operations-panel">
      <div className="panel-header">
        <div>
          <h2>Recent operations</h2>
          <p>
            The manager exposes operation details by ID, but does not provide an
            operations-list endpoint.
          </p>
        </div>
      </div>
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Operation</th>
              <th>VM</th>
              <th>Status</th>
              <th>Updated</th>
              <th>Error</th>
            </tr>
          </thead>
          <tbody>
            {operations.length === 0 ? (
              <tr>
                <td colSpan={5}>
                  <div className="empty-state">
                    <Check size={24} />
                    <strong>No operations in this session</strong>
                    <span>
                      Lifecycle actions and VM creation will appear here.
                    </span>
                  </div>
                </td>
              </tr>
            ) : (
              operations.map((operation) => (
                <tr key={operation.operationId}>
                  <td>
                    <span className="operation-type">{operation.action}</span>
                    <span className="operation-id">
                      {operation.operationId}
                    </span>
                  </td>
                  <td>
                    {names.get(operation.vmId) ||
                      operation.vmName ||
                      operation.vmId}
                  </td>
                  <td>
                    <StatusBadge state={operation.status} variant="operation" />
                  </td>
                  <td>{formatTime(operation.updatedAt)}</td>
                  <td className="operation-error">{operation.error || "—"}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      <div className="table-footer">
        <span>
          {operations.length} tracked operation
          {operations.length === 1 ? "" : "s"} · This list is local to the
          current session.
        </span>
      </div>
    </section>
  );
}

export default App;
