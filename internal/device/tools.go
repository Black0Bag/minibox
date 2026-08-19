package device

// ToolDef MCP 工具定义（D-09：18 个设备_* 工具）。
type ToolDef struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Parameters  any     `json:"parameters"`
	Category    string  `json:"category"` // 眼/手/口/耳
}

// DeviceTools 18 个 MCP 设备工具（D-09/D-10）。
var DeviceTools = []ToolDef{
	// 眼（视觉感知）
	{
		Name: "设备_截屏", Description: "截取设备当前屏幕",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"quality": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}, "default": "medium"}}},
		Category: "眼",
	},
	{
		Name: "设备_界面层级", Description: "获取当前界面 UI 层级树",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
		Category: "眼",
	},
	{
		Name: "设备_通知列表", Description: "获取设备通知列表",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"max": map[string]any{"type": "integer", "default": 10}}},
		Category: "眼",
	},
	{
		Name: "设备_剪贴板", Description: "读取设备剪贴板内容",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
		Category: "眼",
	},
	{
		Name: "设备_信息", Description: "获取设备基本信息（型号/系统/电量）",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
		Category: "眼",
	},
	{
		Name: "设备_前台应用", Description: "获取当前前台应用信息",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
		Category: "眼",
	},

	// 手（操作执行）
	{
		Name: "设备_点击", Description: "点击屏幕指定坐标",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "integer"}, "y": map[string]any{"type": "integer"}, "duration_ms": map[string]any{"type": "integer", "default": 80}}, "required": []string{"x", "y"}},
		Category: "手",
	},
	{
		Name: "设备_滑动", Description: "从起点滑动到终点",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"x1": map[string]any{"type": "integer"}, "y1": map[string]any{"type": "integer"}, "x2": map[string]any{"type": "integer"}, "y2": map[string]any{"type": "integer"}, "duration_ms": map[string]any{"type": "integer", "default": 300}}, "required": []string{"x1", "y1", "x2", "y2"}},
		Category: "手",
	},
	{
		Name: "设备_长按", Description: "长按屏幕指定坐标",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "integer"}, "y": map[string]any{"type": "integer"}, "duration_ms": map[string]any{"type": "integer", "default": 1000}}, "required": []string{"x", "y"}},
		Category: "手",
	},
	{
		Name: "设备_输入文字", Description: "输入文字到当前焦点",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}},
		Category: "手",
	},
	{
		Name: "设备_按键", Description: "模拟按键",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"key": map[string]any{"type": "string", "enum": []string{"home", "back", "recent", "enter", "power", "volume_up", "volume_down"}}}, "required": []string{"key"}},
		Category: "手",
	},
	{
		Name: "设备_打开应用", Description: "打开指定应用",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"package_name": map[string]any{"type": "string"}}, "required": []string{"package_name"}},
		Category: "手",
	},
	{
		Name: "设备_滚动查找", Description: "滚动屏幕查找指定文本",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}, "direction": map[string]any{"type": "string", "enum": []string{"up", "down"}, "default": "down"}}, "required": []string{"text"}},
		Category: "手",
	},

	// 口（语音输出）
	{
		Name: "设备_朗读", Description: "让设备朗读指定文本",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}},
		Category: "口",
	},
	{
		Name: "设备_提示", Description: "在设备上显示提示消息",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}, "duration_sec": map[string]any{"type": "integer", "default": 3}}, "required": []string{"message"}},
		Category: "口",
	},

	// 耳（监听感知）
	{
		Name: "设备_通知", Description: "发送通知到设备",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}}, "required": []string{"title", "text"}},
		Category: "耳",
	},
	{
		Name: "设备_听写", Description: "开始听写（语音转文字）",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"duration_sec": map[string]any{"type": "integer", "default": 10}},
"required": []string{}},
		Category: "耳",
	},
	{
		Name: "设备_监听", Description: "监听设备主动事件",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{"events": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"events"}},
		Category: "耳",
	},
}