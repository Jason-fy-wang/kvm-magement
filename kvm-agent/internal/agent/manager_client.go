package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
)

type managerClient struct {
	url, agentID, publicURL string
	hub                     *eventHub
	logger                  *slog.Logger
}

func newManagerClient(url, agentID, publicURL string, hub *eventHub, logger *slog.Logger) *managerClient {
	return &managerClient{url: trimURL(url), agentID: agentID, publicURL: trimURL(publicURL), hub: hub, logger: logger}
}

func (c *managerClient) Run(ctx context.Context) {
	for {
		if err := c.connect(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Warn("manager websocket disconnected", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
				c.logger.Debug("retrying manager WebSocket connection", "agent_id", c.agentID)
			}
		}
	}
}

func (c *managerClient) connect(ctx context.Context) error {
	c.logger.Debug("connecting to manager WebSocket", "url", c.url, "agent_id", c.agentID)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.url, nil)
	if err != nil {
		c.logger.Warn("manager WebSocket connection failed", "url", c.url, "agent_id", c.agentID, "error", err)
		return err
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "agent.register", "agent_id": c.agentID, "url": c.publicURL, "timestamp": time.Now().UTC()}); err != nil {
		c.logger.Warn("manager registration failed", "agent_id", c.agentID, "error", err)
		return err
	}
	c.logger.Info("registered with manager", "agent_id", c.agentID, "manager_url", c.url, "public_url", c.publicURL)
	sub := c.hub.subscribe()
	defer c.hub.unsubscribe(sub)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-sub:
			if err := conn.WriteJSON(event); err != nil {
				c.logger.Warn("send event to manager failed", "agent_id", c.agentID, "event_type", event.Type, "vm_id", event.VMID, "error", err)
				return err
			}
			c.logger.Debug("event sent to manager", "agent_id", c.agentID, "event_type", event.Type, "vm_id", event.VMID)
		}
	}
}
