import type { VMState } from "../api/types";

function stateTone(state: VMState): string {
  if (state === "running" || state === "online") return "success";
  if (state === "error" || state === "offline" || state === "failed")
    return "danger";
  if (state === "creating" || state === "unknown" || state === "queued")
    return "warning";
  return "muted";
}

export function StatusBadge({
  state,
  variant,
}: {
  state: string;
  variant?: "operation";
}) {
  const tone =
    variant === "operation" && state === "running"
      ? "warning"
      : stateTone(state);
  return (
    <span className={`status-pill ${tone}`}>
      <span className="status-dot" />
      {state}
    </span>
  );
}
