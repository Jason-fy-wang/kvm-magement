import { AlertCircle, Check, X } from "lucide-react";

export interface ToastMessage {
  tone: "success" | "danger" | "info";
  message: string;
}

export function Toast({
  toast,
  onDismiss,
}: {
  toast: ToastMessage;
  onDismiss: () => void;
}) {
  return (
    <div
      className={`toast ${toast.tone}`}
      role={toast.tone === "danger" ? "alert" : "status"}
    >
      <span className="toast-icon">
        {toast.tone === "danger" ? (
          <AlertCircle size={15} />
        ) : (
          <Check size={15} />
        )}
      </span>
      <span>{toast.message}</span>
      <button aria-label="Dismiss notification" onClick={onDismiss}>
        <X size={14} />
      </button>
    </div>
  );
}
