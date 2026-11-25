import type { ReactNode } from "react";

export interface Metric {
  label: string;
  value: string;
  detail: string;
  icon: ReactNode;
  tone: string;
}

export function MetricCard({ label, value, detail, icon, tone }: Metric) {
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
