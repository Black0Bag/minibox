package ws

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegisterDefaultHandlers(t *testing.T) {
	s := New(nil)
	s.RegisterDefaultHandlers()

	// Verify all expected routes are registered
	expected := []string{
		"device", "browser",
		MethodPeerDiscover, MethodPeerRelay,
		MethodEventAck,
		MethodSystemInfo, MethodSystemStats, MethodSystemLog,
	}
	for _, m := range expected {
		if _, ok := s.routes[m]; !ok {
			t.Errorf("route %q not registered", m)
		}
	}
}

func TestHandleSystemInfo(t *testing.T) {
	s := New(nil)
	c := &Client{ID: "test-client"}
	result, err := s.handleSystemInfo(context.Background(), c, nil)
	if err != nil {
		t.Fatalf("handleSystemInfo failed: %v", err)
	}
	m := result.(map[string]any)
	if m["version"] != "1.0" {
		t.Errorf("version = %v", m["version"])
	}
	if m["client_id"] != "test-client" {
		t.Errorf("client_id = %v", m["client_id"])
	}
}

func TestHandleSystemStats(t *testing.T) {
	s := New(nil)
	s.RegisterDefaultHandlers()
	result, err := s.handleSystemStats(context.Background(), &Client{}, nil)
	if err != nil {
		t.Fatalf("handleSystemStats failed: %v", err)
	}
	m := result.(map[string]any)
	if _, ok := m["clients"]; !ok {
		t.Error("stats missing clients field")
	}
	if _, ok := m["routes"]; !ok {
		t.Error("stats missing routes field")
	}
}

func TestHandleDeviceProxy(t *testing.T) {
	s := New(nil)
	result, err := s.handleDeviceProxy(context.Background(), &Client{}, nil)
	if err != nil {
		t.Fatalf("handleDeviceProxy failed: %v", err)
	}
	m := result.(map[string]any)
	if m["ok"] != true {
		t.Error("device proxy should return ok=true")
	}
}

func TestHandleBrowserProxy(t *testing.T) {
	s := New(nil)
	result, err := s.handleBrowserProxy(context.Background(), &Client{}, nil)
	if err != nil {
		t.Fatalf("handleBrowserProxy failed: %v", err)
	}
	m := result.(map[string]any)
	if m["ok"] != true {
		t.Error("browser proxy should return ok=true")
	}
}

func TestHandlePeerRelay(t *testing.T) {
	s := New(nil)
	params, _ := json.Marshal(map[string]string{"peer_id": "peer-1"})
	result, err := s.handlePeerRelay(context.Background(), &Client{}, params)
	if err != nil {
		t.Fatalf("handlePeerRelay failed: %v", err)
	}
	m := result.(map[string]any)
	if m["peer_id"] != "peer-1" {
		t.Errorf("peer_id = %v", m["peer_id"])
	}
}

func TestHandlePeerRelayMissingPeerID(t *testing.T) {
	s := New(nil)
	_, err := s.handlePeerRelay(context.Background(), &Client{}, nil)
	if err == nil {
		t.Error("should error when peer_id missing")
	}
}

func TestHandleEventAck(t *testing.T) {
	s := New(nil)
	params, _ := json.Marshal(map[string]string{"event_id": "evt-1"})
	result, err := s.handleEventAck(context.Background(), &Client{}, params)
	if err != nil {
		t.Fatalf("handleEventAck failed: %v", err)
	}
	m := result.(map[string]any)
	if m["event_id"] != "evt-1" {
		t.Errorf("event_id = %v", m["event_id"])
	}
}

func TestLookupPrefixRouting(t *testing.T) {
	s := New(nil)
	s.RegisterDefaultHandlers()

	// device.* should route to "device" prefix handler
	fn, ok := s.lookup("device.camera.photo")
	if !ok {
		t.Error("prefix routing for device.* failed")
	}
	_ = fn

	// browser.* should route to "browser" prefix handler
	fn2, ok := s.lookup("browser.tab.open")
	if !ok {
		t.Error("prefix routing for browser.* failed")
	}
	_ = fn2

	// Exact match should work for peer.discover
	fn3, ok := s.lookup("peer.discover")
	if !ok {
		t.Error("exact match for peer.discover failed")
	}
	_ = fn3
}

func TestMethodConstants(t *testing.T) {
	// Verify method constant names match design doc
	if MethodDeviceScreenCapture != "device.screen.capture" {
		t.Errorf("MethodDeviceScreenCapture = %q", MethodDeviceScreenCapture)
	}
	if MethodBrowserTabOpen != "browser.tab.open" {
		t.Errorf("MethodBrowserTabOpen = %q", MethodBrowserTabOpen)
	}
	if MethodPeerDiscover != "peer.discover" {
		t.Errorf("MethodPeerDiscover = %q", MethodPeerDiscover)
	}
	if MethodEventPush != "event.push" {
		t.Errorf("MethodEventPush = %q", MethodEventPush)
	}
}
