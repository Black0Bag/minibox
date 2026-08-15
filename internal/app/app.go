// Package app 是组合根（composition root）。
// 这是唯一能看到所有模块的地方：装配依赖、跨模块翻译。
// 模块之间绝不直接 import，全部通过这里调解。
package app

import (
	"context"
	"log/slog"

	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/domain/tools"
	httptransport "github.com/Black0Bag/minibox/internal/transport/http"
	ssetransport "github.com/Black0Bag/minibox/internal/transport/sse"
	wstransport "github.com/Black0Bag/minibox/internal/transport/ws"
)

// App 是应用根对象，持有所有装配好的依赖。
type App struct {
	cfg     config.Config
	logger  *slog.Logger
	http    *httptransport.Server
	sse     *ssetransport.Server
	ws      *wstransport.Server
	toolReg *tools.Registry
}

// New 创建 App，装配所有依赖。
// 传输层（Phase 6）在组合根装配：HTTP/SSE/WS 三通道共用统一信封。
func New(_ context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	// 工具注册表（Phase 5 模块27，供 /tools 端点）
	reg := tools.NewRegistry()

	a := &App{
		cfg:     cfg,
		logger:  logger,
		toolReg: reg,
		// 传输层装配（三通道独立路由，共用 domain/transport.Envelope）
		http: httptransport.New(cfg.Server, logger, reg),
		sse:  ssetransport.New(logger),
		ws:   wstransport.New(logger),
	}
	return a, nil
}

// Config 返回应用配置。
func (a *App) Config() config.Config {
	return a.cfg
}

// Logger 返回应用日志器。
func (a *App) Logger() *slog.Logger {
	return a.logger
}

// ToolRegistry 返回工具注册表（供 Phase 5 装配内置工具）。
func (a *App) ToolRegistry() *tools.Registry {
	return a.toolReg
}

// SSE 返回 SSE 传输服务器（供 B14 主动推送）。
func (a *App) SSE() *ssetransport.Server {
	return a.sse
}

// WS 返回 WebSocket 传输服务器（供设备代理）。
func (a *App) WS() *wstransport.Server {
	return a.ws
}

// Run 启动应用。
// 当前：仅启动 HTTP 传输层（REST + SSE + WS 共用一个端口）。
func (a *App) Run(ctx context.Context) error {
	a.logger.Info("minibox 启动",
		"port", a.cfg.Server.Port,
		"listen", a.cfg.Server.Listen,
	)

	errCh := make(chan error, 1)
	go func() {
		// HTTP 服务（内部 mount REST 路由；SSE/WS 独立路由组可后续挂载）
		if err := a.http.Start(); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return nil
	}
}

// Close 优雅关闭。
func (a *App) Close() error {
	a.logger.Info("minibox 关闭")
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Server.ShutdownTimeout)
	defer cancel()
	return a.http.Shutdown(ctx)
}
