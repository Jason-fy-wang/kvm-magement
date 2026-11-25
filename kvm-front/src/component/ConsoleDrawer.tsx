import { FormEvent, KeyboardEvent, useEffect, useRef, useState } from "react";
import { Send, X } from "lucide-react";
import type { VM } from "../api/types";
import { managerApi } from "../api/manager";

export function ConsoleDrawer({
  vm,
  onClose,
}: {
  vm: VM;
  onClose: () => void;
}) {
  const [connected, setConnected] = useState(false);
  const [lines, setLines] = useState<string[]>([]);
  const [input, setInput] = useState("");
  const [error, setError] = useState("");
  const socketRef = useRef<WebSocket | null>(null);
  const outputRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const socket = new WebSocket(managerApi.consoleURL(vm.id));
    socketRef.current = socket;
    socket.onopen = () => {
      setConnected(true);
      setError("");
    };
    socket.onmessage = (event) => {
      setLines((current) => [...current, String(event.data)].slice(-1000));
    };
    socket.onerror = () =>
      setError("Console connection failed or is unavailable.");
    socket.onclose = () => {
      setConnected(false);
      socketRef.current = null;
    };
    return () => socket.close();
  }, [vm.id]);

  useEffect(() => {
    outputRef.current?.scrollTo(0, outputRef.current.scrollHeight);
  }, [lines]);

  function sendInput(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const socket = socketRef.current;
    if (!socket || socket.readyState !== WebSocket.OPEN || !input) return;
    socket.send(`${input}\n`);
    setInput("");
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      event.currentTarget.form?.requestSubmit();
    }
  }

  return (
    <div
      className="drawer-backdrop"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <section
        className="drawer console-drawer"
        role="dialog"
        aria-modal="true"
        aria-labelledby="console-title"
      >
        <div className="drawer-header">
          <div>
            <div className="eyebrow">Serial console</div>
            <h2 id="console-title">{vm.name}</h2>
          </div>
          <div className="console-heading-actions">
            <span className={`console-status ${connected ? "connected" : ""}`}>
              <span />
              {connected ? "Connected" : "Disconnected"}
            </span>
            <button
              className="close-button"
              aria-label="Close console"
              onClick={onClose}
            >
              <X size={18} />
            </button>
          </div>
        </div>
        {error && (
          <div className="console-error" role="alert">
            {error}
          </div>
        )}
        <div className="terminal-window">
          <div className="terminal-top">
            <span />
            <span />
            <span />
            <small>{vm.id} / ttyS0</small>
          </div>
          <div className="terminal-body" ref={outputRef} aria-live="polite">
            {lines.length ? (
              lines.map((line, index) => (
                <span className="terminal-line" key={index}>
                  {line}
                </span>
              ))
            ) : (
              <span className="terminal-line">Waiting for serial output…</span>
            )}
          </div>
          <form className="console-input" onSubmit={sendInput}>
            <label className="sr-only" htmlFor="console-command">
              Send console input
            </label>
            <input
              id="console-command"
              value={input}
              onChange={(event) => setInput(event.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Type to send to serial console…"
              disabled={!connected}
            />
            <button
              type="submit"
              aria-label="Send console input"
              disabled={!connected || !input}
            >
              <Send size={15} />
            </button>
          </form>
        </div>
        <div className="console-footer">
          <span>
            Raw Firecracker serial stream. Guest login depends on the image.
          </span>
          <span>Output is displayed as plain text.</span>
        </div>
      </section>
    </div>
  );
}
