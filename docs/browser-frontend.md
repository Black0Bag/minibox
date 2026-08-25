# 浏览器代理 - 前端交叉互动设计

## 设计理念

> **后端不跑浏览器，浏览器能力交给前端。**

原因：
1. 后端服务器可能是低性能设备（路由器、N1 盒子），跑浏览器会拖垮系统
2. 前端是用户最直接的交互界面，用户可以实时看到 Agent 在做什么
3. 遇到登录、验证码等特殊情况，用户可以手动介入辅助
4. 前端设备（手机）通常性能更好，浏览器功能更完整

## 前后端分工

| 角色 | 职责 |
|---|---|
| 后端 | 发出"我要操作浏览器"的指令，等待结果 |
| 前端 | 实际执行浏览器操作，返回结果给后端 |

## 通信协议（WebSocket JSON-RPC）

后端通过 WebSocket 向前端发送 `browser.*` 方法请求：

```json
{
  "jsonrpc": "2.0",
  "id": "browser_001",
  "method": "browser",
  "params": {
    "method": "open",
    "params": {
      "url": "https://example.com",
      "tab_id": "tab_001"
    }
  }
}
```

前端执行后返回：

```json
{
  "jsonrpc": "2.0",
  "id": "browser_001",
  "result": {
    "ok": true,
    "data": {
      "title": "Example Domain",
      "screenshot": "base64..."
    }
  }
}
```

## 前端需实现的能力

| 能力 | 说明 |
|---|---|
| 打开/关闭 Tab | 多标签页管理 |
| 导航（前进/后退/刷新） | 浏览器基本操作 |
| 截图 | 返回当前页面截图（base64） |
| 点击/输入 | 模拟用户操作 |
| 获取 DOM 文本 | 提取页面内容 |
| 执行 JavaScript | 注入并执行 JS 代码 |
| 获取 Cookie | 用于登录态管理 |
| 用户手动介入 | 登录、验证码等场景暂停，等待用户操作 |

## 前端技术实现建议（Android）

- **WebView**：Android 原生 WebView 即可满足需求
- **JavaScript 注入**：通过 `evaluateJavascript` 执行 JS
- **截图**：通过 `WebView.draw()` 或 `PixelCopy` 获取
- **多标签页**：多个 WebView 实例，或使用 `WebView.copyBackForwardList` 管理
- **前后台切换**：使用 Android 前台服务保持 WebView 存活

## 后端占位现状

当前后端 `internal/transport/ws/handlers.go` 中 `handleBrowserProxy` 返回占位消息：

```go
func (s *Server) handleBrowserProxy(...) {
    return map[string]any{
        "ok":      true,
        "message": "browser proxy: awaiting real browser implementation",
    }, nil
}
```

前端实现后，后端只需将 `browser.*` 请求通过 WebSocket 转发给前端，不再自己执行。

## 安全注意事项

1. 浏览器操作指令需要认证（走现有设备 WebSocket 认证）
2. 禁止访问内网敏感地址（白名单/黑名单机制）
3. 截图中敏感信息（密码、验证码）前端自行脱敏
4. 用户手动介入时，需明确提示"Agent 正在操作浏览器"