package device

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Black0Bag/minibox/internal/domain/tools"
)

type commandToolTestSender struct {
	deviceID string
	method   string
	params   json.RawMessage
}

func (s *commandToolTestSender) SendCommand(_ context.Context, deviceID, method string, params json.RawMessage) (*CommandResult, error) {
	s.deviceID, s.method, s.params = deviceID, method, append(json.RawMessage(nil), params...)
	return &CommandResult{OK: true, Data: `{"ok":true}`}, nil
}

func TestNewCommandToolsRegistersAllDeviceCapabilities(t *testing.T) {
	sender := &commandToolTestSender{}
	got := NewCommandTools(sender)
	if len(got) != len(DeviceTools) || len(got) != 18 {
		t.Fatalf("tool count=%d, want 18", len(got))
	}
	for _, tool := range got {
		if !strings.HasPrefix(tool.Name(), "设备_") {
			t.Fatalf("unexpected tool name=%q", tool.Name())
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.JSONSchema(), &schema); err != nil {
			t.Fatalf("tool %s schema invalid: %v", tool.Name(), err)
		}
	}
}

func TestCommandToolInvokeRequiresDeviceIDAndForwardsParams(t *testing.T) {
	sender := &commandToolTestSender{}
	tool := NewCommandTools(sender)[0] // 设备_截屏
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"quality":"low"}`)); err == nil {
		t.Fatal("缺少 device_id 应失败")
	}
	out, err := tool.Invoke(context.Background(), json.RawMessage(`{"device_id":"dev-1","quality":"low"}`))
	if err != nil || out != `{"ok":true}` {
		t.Fatalf("invoke out=%q err=%v", out, err)
	}
	if sender.deviceID != "dev-1" || sender.method != "设备_截屏" || string(sender.params) != `{"quality":"low"}` {
		t.Fatalf("forwarded deviceID=%q method=%q params=%s", sender.deviceID, sender.method, sender.params)
	}
}

func TestCommandToolMetadataFailClosedForWrite(t *testing.T) {
	var write tools.Tool
	for _, candidate := range NewCommandTools(&commandToolTestSender{}) {
		if candidate.Name() == "设备_点击" {
			write = candidate
			break
		}
	}
	if write == nil {
		t.Fatal("未找到设备_点击工具")
	}
	if !write.Metadata().RequiresApproval || !write.Metadata().Destructive {
		t.Fatalf("write metadata=%+v", write.Metadata())
	}
}
