import { FormEvent, useState } from "react";
import { LoaderCircle, Network, Plus, X } from "lucide-react";
import type { Agent, CreateVMRequest } from "../api/types";

interface CreateVMDialogProps {
  agents: Agent[];
  submitting: boolean;
  onClose: () => void;
  onSubmit: (
    input: CreateVMRequest,
    startAfterCreate: boolean,
  ) => Promise<void>;
}

const initialForm: CreateVMRequest = {
  name: "",
  agent_id: "",
  vcpu_count: 2,
  memory_mib: 256,
  kernel_path: "",
  rootfs_path: "",
  disk_path: "",
  tap_device: "",
};

export function CreateVMDialog({
  agents,
  submitting,
  onClose,
  onSubmit,
}: CreateVMDialogProps) {
  const [form, setForm] = useState(initialForm);
  const [startAfterCreate, setStartAfterCreate] = useState(false);
  const update = <K extends keyof CreateVMRequest>(
    key: K,
    value: CreateVMRequest[K],
  ) => setForm((current) => ({ ...current, [key]: value }));

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void onSubmit(
      {
        ...form,
        agent_id: form.agent_id || undefined,
        disk_path: form.disk_path || undefined,
        tap_device: form.tap_device || undefined,
        vcpu_count: Number(form.vcpu_count),
        memory_mib: Number(form.memory_mib),
      },
      startAfterCreate,
    );
  }

  return (
    <div
      className="drawer-backdrop"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <section
        className="drawer create-drawer"
        role="dialog"
        aria-modal="true"
        aria-labelledby="create-vm-title"
      >
        <div className="drawer-header">
          <div>
            <div className="eyebrow">Provisioning</div>
            <h2 id="create-vm-title">Create virtual machine</h2>
          </div>
          <button
            className="close-button"
            aria-label="Close dialog"
            onClick={onClose}
          >
            <X size={18} />
          </button>
        </div>
        <form onSubmit={submit} className="drawer-form">
          <div className="form-section">
            <div className="form-section-title">Identity</div>
            <div className="field-grid">
              <label className="field">
                <span>VM name</span>
                <input
                  required
                  value={form.name}
                  onChange={(event) => update("name", event.target.value)}
                  placeholder="e.g. api-worker-01"
                />
              </label>
              <label className="field">
                <span>Physical host</span>
                <select
                  value={form.agent_id}
                  onChange={(event) => update("agent_id", event.target.value)}
                >
                  <option value="">Automatic placement</option>
                  {agents
                    .filter((agent) => agent.status === "online")
                    .map((agent) => (
                      <option key={agent.id} value={agent.id}>
                        {agent.hostname || agent.id}
                      </option>
                    ))}
                </select>
                <small>
                  Leave automatic to let the manager choose an online host.
                </small>
              </label>
            </div>
          </div>
          <div className="form-section">
            <div className="form-section-title">Compute</div>
            <div className="field-grid">
              <label className="field">
                <span>vCPU</span>
                <input
                  required
                  min="1"
                  type="number"
                  value={form.vcpu_count}
                  onChange={(event) =>
                    update("vcpu_count", Number(event.target.value))
                  }
                />
              </label>
              <label className="field">
                <span>Memory (MiB)</span>
                <input
                  required
                  min="128"
                  type="number"
                  value={form.memory_mib}
                  onChange={(event) =>
                    update("memory_mib", Number(event.target.value))
                  }
                />
              </label>
            </div>
          </div>
          <div className="form-section">
            <div className="form-section-title">Boot media</div>
            <label className="field">
              <span>Kernel image path</span>
              <input
                required
                value={form.kernel_path}
                onChange={(event) => update("kernel_path", event.target.value)}
                placeholder="/path/on/host/vmlinux"
              />
            </label>
            <label className="field">
              <span>Root filesystem path</span>
              <input
                required
                value={form.rootfs_path}
                onChange={(event) => update("rootfs_path", event.target.value)}
                placeholder="/path/on/host/rootfs.ext4"
              />
            </label>
            <label className="field">
              <span>Additional disk path (optional)</span>
              <input
                value={form.disk_path}
                onChange={(event) => update("disk_path", event.target.value)}
                placeholder="/path/on/host/data.ext4"
              />
            </label>
          </div>
          <div className="form-section">
            <div className="form-section-title">Network</div>
            <label className="field">
              <span>TAP device (optional)</span>
              <input
                value={form.tap_device}
                onChange={(event) => update("tap_device", event.target.value)}
                placeholder="tap-vm-name"
              />
            </label>
            <div className="network-note">
              <Network size={15} />
              <span>
                The manager allocates the guest IP, MAC, gateway, and netmask.
              </span>
            </div>
          </div>
          <label className="automation-option">
            <input
              type="checkbox"
              checked={startAfterCreate}
              onChange={(event) => setStartAfterCreate(event.target.checked)}
            />
            <span>
              <strong>Start after create</strong>
              <small>
                Wait for provisioning to succeed before starting the VM.
              </small>
            </span>
          </label>
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
                  <LoaderCircle size={16} className="spin" /> Creating
                </>
              ) : (
                <>
                  <Plus size={16} /> Create VM
                </>
              )}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}
