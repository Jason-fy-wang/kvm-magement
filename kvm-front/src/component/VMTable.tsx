import {
  Activity,
  CircleDot,
  Monitor,
  Power,
  Search,
  TerminalSquare,
  Trash2,
} from "lucide-react";
import { useMemo, useState } from "react";
import type { VM, VMAction } from "../api/types";
import { formatTime } from "../utils/format";
import { StatusBadge } from "./StatusBadge";

interface VMTableProps {
  vms: VM[];
  loading: boolean;
  error: string;
  stateFilter: string;
  onRetry: () => void;
  onStateFilterChange: (state: string) => void;
  onSelect: (vm: VM) => void;
  onAction: (vm: VM, action: VMAction) => void;
  onConsole: (vm: VM) => void;
}

export function VMTable({
  vms,
  loading,
  error,
  stateFilter,
  onRetry,
  onStateFilterChange,
  onSelect,
  onAction,
  onConsole,
}: VMTableProps) {
  const [search, setSearch] = useState("");
  const filteredVMs = useMemo(() => {
    const query = search.toLowerCase().trim();
    return vms.filter((vm) => {
      if (stateFilter !== "all" && vm.state !== stateFilter) return false;
      return (
        !query ||
        [vm.name, vm.id, vm.guest_ip, vm.agent_id].some((value) =>
          value?.toLowerCase().includes(query),
        )
      );
    });
  }, [search, stateFilter, vms]);

  return (
    <div className="table-panel">
      <div className="panel-header">
        <div>
          <h2>Virtual machines</h2>
          <p>Every VM registered with the control plane</p>
        </div>
        <div className="panel-actions">
          <label className="search-box">
            <Search size={15} />
            <input
              aria-label="Search VMs"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Search VMs"
            />
          </label>
          <select
            aria-label="Filter VMs by state"
            value={stateFilter}
            onChange={(event) => onStateFilterChange(event.target.value)}
          >
            <option value="all">All states</option>
            <option value="running">Running</option>
            <option value="stopped">Stopped</option>
            <option value="creating">Creating</option>
            <option value="error">Error</option>
            <option value="unknown">Unknown</option>
          </select>
        </div>
      </div>
      {error && (
        <div className="error-banner" role="alert">
          <span>{error}</span>
          <button onClick={onRetry}>Retry</button>
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
                  <div className="loading-state">Loading inventory…</div>
                </td>
              </tr>
            ) : filteredVMs.length === 0 ? (
              <tr>
                <td colSpan={7}>
                  <div className="empty-state">
                    <Monitor size={24} />
                    <strong>
                      {vms.length ? "No matching VMs" : "No VMs registered"}
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
              filteredVMs.map((vm) => {
                const canStart = vm.state === "stopped" || vm.state === "error";
                const canStop = vm.state === "running";
                const canDelete =
                  vm.state !== "creating" && vm.state !== "deleted";
                return (
                  <tr
                    className="vm-row"
                    key={vm.id}
                    onClick={() => onSelect(vm)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter") onSelect(vm);
                    }}
                    tabIndex={0}
                  >
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
                      <StatusBadge state={vm.state} />
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
                      <span className="updated-cell">
                        {formatTime(vm.updated_at)}
                      </span>
                    </td>
                    <td>
                      <div
                        className="row-actions"
                        onClick={(event) => event.stopPropagation()}
                      >
                        {vm.state === "running" && (
                          <button
                            aria-label={`Open ${vm.name} console`}
                            title="Open console"
                            onClick={() => onConsole(vm)}
                          >
                            <TerminalSquare size={15} />
                          </button>
                        )}
                        {(canStart || canStop) && (
                          <button
                            aria-label={`${canStart ? "Start" : "Stop"} ${vm.name}`}
                            title={canStart ? "Start VM" : "Stop VM"}
                            onClick={() =>
                              onAction(vm, canStart ? "start" : "stop")
                            }
                          >
                            <Power size={15} />
                          </button>
                        )}
                        {canDelete && (
                          <button
                            aria-label={`Delete ${vm.name}`}
                            title="Delete VM"
                            onClick={() => onAction(vm, "delete")}
                          >
                            <Trash2 size={15} />
                          </button>
                        )}
                        {vm.state === "running" && (
                          <Activity
                            className="row-running-icon"
                            size={14}
                            aria-label="VM running"
                          />
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })
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
          Manager inventory
        </span>
      </div>
    </div>
  );
}
