package app

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Black0Bag/minibox/internal/device"
	wstransport "github.com/Black0Bag/minibox/internal/transport/ws"
)

// handleDeviceMessage WS 设备消息分发（D-02/D-05/D-07/D-08）。
func (a *App) handleDeviceMessage(ctx context.Context, c *wstransport.Client, params json.RawMessage) (any, error) {
	var msg struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params,omitempty"`
	}
	if err := json.Unmarshal(params, &msg); err != nil {
		return nil, err
	}

	switch msg.Method {
	case "connect":
		return a.handleDeviceConnect(ctx, c, msg.Params)
	case "disconnect":
		return a.handleDeviceDisconnect(ctx, c)
	case "hello":
		return a.handleDeviceHello(ctx, c, msg.Params)
	case "event":
		return a.handleDeviceEvent(ctx, c, msg.Params)
	default:
		return nil, nil
	}
}

func (a *App) handleDeviceConnect(ctx context.Context, c *wstransport.Client, params json.RawMessage) (any, error) {
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.DeviceID == "" {
		return nil, nil
	}

	dev, err := a.hub.HandleConnect(ctx, c, req.DeviceID, "", "", nil, nil)
	if err != nil {
		return nil, err
	}
	c.SetData(dev)
	return map[string]any{"ok": true, "device_id": dev.ID}, nil
}

func (a *App) handleDeviceDisconnect(_ context.Context, c *wstransport.Client) (any, error) {
	if dev, ok := c.Data().(*device.Device); ok {
		a.hub.HandleDisconnect(dev.ID)
	}
	return map[string]any{"ok": true}, nil
}

func (a *App) handleDeviceHello(ctx context.Context, c *wstransport.Client, params json.RawMessage) (any, error) {
	var info struct {
		ID           string          `json:"id"`
		Model        string          `json:"model"`
		Android      string          `json:"android"`
		Capabilities []string        `json:"capabilities"`
		Permissions  json.RawMessage `json:"permissions"`
	}
	if err := json.Unmarshal(params, &info); err != nil {
		return nil, nil
	}

	dev, err := a.hub.HandleConnect(ctx, c, info.ID, info.Model, info.Android, info.Capabilities, nil)
	if err != nil {
		return nil, err
	}
	c.SetData(dev)
	return map[string]any{"ok": true, "device_id": dev.ID}, nil
}

func (a *App) handleDeviceEvent(_ context.Context, _ *wstransport.Client, params json.RawMessage) (any, error) {
	var evt device.Event
	if err := json.Unmarshal(params, &evt); err != nil {
		return nil, nil
	}
	a.logger.Info("设备事件", "event", evt.Event, "data", evt.Data)
	return map[string]any{"ok": true}, nil
}

// mountDeviceREST 挂载设备管理 REST 端点（D-11）。
func (a *App) mountDeviceREST(r chi.Router) {
	r.Route("/api/v1/device", func(r chi.Router) {
		r.Get("/", a.handleDeviceList)
		r.Get("/{id}/status", a.handleDeviceStatus)
		r.Get("/{id}/commands", a.handleDeviceCommands)
		r.Post("/{id}/unbind", a.handleDeviceUnbind)
		r.Post("/{id}/emergency-stop", a.handleDeviceEmergencyStop)
		r.Post("/pair", a.handleDevicePair)
	})
}

func (a *App) handleDeviceList(w http.ResponseWriter, r *http.Request) {
	devices := a.hub.ListDevices()
	a.respondOK(w, r, "api.device.list", map[string]any{"devices": devices})
}

func (a *App) handleDeviceStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	dev := a.hub.GetDevice(id)
	if dev == nil {
		a.respondErr(w, r, http.StatusNotFound, "not_found", "设备不存在: "+id)
		return
	}
	a.respondOK(w, r, "api.device.status", dev)
}

// handleDeviceCommands 返回指定设备的审计命令。
func (a *App) handleDeviceCommands(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if a.hub.GetDevice(id) == nil {
		a.respondErr(w, r, http.StatusNotFound, "not_found", "设备不存在: "+id)
		return
	}
	all := a.hub.AuditLog()
	logs := make([]*device.Command, 0, len(all))
	for _, cmd := range all {
		if cmd.DeviceID == id {
			logs = append(logs, cmd)
		}
	}
	a.respondOK(w, r, "api.device.commands", map[string]any{"commands": logs})
}

func (a *App) handleDeviceUnbind(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	a.hub.RemoveDevice(id)
	a.respondOK(w, r, "api.device.unbind", map[string]bool{"ok": true})
}

// handleDeviceEmergencyStop 急停指定设备（D-13）。
func (a *App) handleDeviceEmergencyStop(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := a.hub.EmergencyStop(id); err != nil {
		a.respondErr(w, r, http.StatusNotFound, "not_found", err.Error())
		return
	}
	a.respondOK(w, r, "api.device.emergency_stop", map[string]bool{"ok": true})
}

func (a *App) handleDevicePair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		a.respondErr(w, r, http.StatusBadRequest, "bad_request", "code 必填")
		return
	}
	deviceID, ok := a.hub.PairingManager().VerifyCode(req.Code)
	if !ok {
		a.respondErr(w, r, http.StatusBadRequest, "pair_failed", "配对码无效或已过期")
		return
	}
	a.respondOK(w, r, "api.device.pair", map[string]any{"device_id": deviceID, "paired": true})
}
