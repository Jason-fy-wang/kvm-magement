import { Power, RefreshCw, TerminalSquare, Trash2, X } from "lucide-react";
import type { VM, VMAction } from "../api/types";
import { formatTime } from "../utils/format";
import { StatusBadge } from "./StatusBadge";

interface VMDetailDrawerProps {
  vm: VM;
  onClose: () => void;
  onAction: (vm: VM, action: VMAction) => void;
  onConsole: (vm: VM) => void;
}

export function VMDetailDrawer({
  vm,
  onClose,
  onAction,
  onConsole,
}: VMDetailDrawerProps) {
  const running = vm.state === "running";
  const canStart = vm.state === "stopped" || vm.state === "error";
  const canDelete = vm.state !== "creating" && vm.state !== "deleted";
  return (
    <div
      className="drawer-backdrop"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <section
        className="drawer detail-drawer"
        role="dialog"
        aria-modal="true"
        aria-labelledby="vm-detail-title"
      >
        <div className="drawer-header">
          <div>
            <div className="eyebrow">Virtual machine</div>
            <h2 id="vm-detail-title">{vm.name || "Unnamed VM"}</h2>
            <span className="drawer-id">{vm.id}</span>
          </div>
          <button
            className="close-button"
            aria-label="Close details"
            onClick={onClose}
          >
            <X size={18} />
          </button>
        </div>
        <div className="detail-state">
          <StatusBadge state={vm.state} />
          <span>Updated {formatTime(vm.updated_at)}</span>
        </div>
        <div className="detail-actions">
          {running && (
            <button className="primary-button" onClick={() => onConsole(vm)}>
              <TerminalSquare size={16} /> Open console
            </button>
          )}
          {canStart && (
            <button
              className="primary-button"
              onClick={() => onAction(vm, "start")}
            >
              <Power size={16} /> Start
            </button>
          )}
          {running && (
            <button
              className="secondary-button"
              onClick={() => onAction(vm, "stop")}
            >
              <Power size={16} /> Stop
            </button>
          )}
          {running && (
            <button
              className="secondary-button"
              onClick={() => onAction(vm, "reboot")}
            >
              <RefreshCw size={15} /> Reboot
            </button>
          )}
          {canDelete && (
            <button
              className="danger-button"
              onClick={() => onAction(vm, "delete")}
            >
              <Trash2 size={16} /> Delete
            </button>
          )}
        </div>
        <section className="detail-section">
          <div className="form-section-title">Configuration</div>
          <DetailLine label="Host" value={vm.agent_id} />
          <DetailLine label="vCPU" value={`${vm.vcpu_count}`} />
          <DetailLine label="Memory" value={`${vm.memory_mib} MiB`} />
          <DetailLine label="TAP" value={vm.tap_device || "—"} />
        </section>
        <section className="detail-section">
          <div className="form-section-title">Network</div>
          <DetailLine label="Guest IP" value={vm.guest_ip || "—"} mono />
          <DetailLine label="MAC address" value={vm.guest_mac || "—"} mono />
          <DetailLine label="Gateway" value={vm.gateway_ip || "—"} mono />
          <DetailLine label="Netmask" value={vm.netmask || "—"} mono />
        </section>
        <section className="detail-section">
          <div className="form-section-title">Storage</div>
          <DetailLine label="Kernel" value={vm.kernel_path} mono />
          <DetailLine label="Root filesystem" value={vm.rootfs_path} mono />
          <DetailLine label="Data disk" value={vm.disk_path || "—"} mono />
        </section>
        <p className="detail-storage-note">
          Deleting a VM does not remove disk files from its host.
        </p>
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
