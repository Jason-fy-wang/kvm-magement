import { FormEvent, useState } from "react";
import { X } from "lucide-react";
import type { VM } from "../api/types";

export function DeleteVMDialog({
  vm,
  onClose,
  onConfirm,
}: {
  vm: VM;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const [confirmation, setConfirmation] = useState("");

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (confirmation === vm.name) onConfirm();
  }

  return (
    <div
      className="drawer-backdrop delete-backdrop"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <section
        className="delete-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="delete-vm-title"
      >
        <div className="delete-dialog-header">
          <div>
            <div className="eyebrow">Confirm deletion</div>
            <h2 id="delete-vm-title">Delete {vm.name}?</h2>
          </div>
          <button
            className="close-button"
            aria-label="Close dialog"
            onClick={onClose}
          >
            <X size={18} />
          </button>
        </div>
        <p>
          This queues VM deletion. Disk files remain on the host. Type the VM
          name to continue.
        </p>
        <form onSubmit={submit}>
          <label className="field">
            <span>VM name</span>
            <input
              autoFocus
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
            />
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
              className="danger-button"
              disabled={confirmation !== vm.name}
              type="submit"
            >
              Delete VM
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}
