// Package app 是组合根（composition root）。
// 这是唯一能看到所有模块的地方：装配依赖、跨模块翻译。
// 模块之间绝不直接 import，全部通过这里调解。
package app

import (
	"context"
	"log/slog"

	"github.com/Black0Bag/minibox/internal/config"
)

// App 是应用根对象，持有所有装配好的依赖。
type App struct {
	cfg    config.Config
	logger *slog.Logger
}

// New 创建 App，装配所有依赖。
// 目前是骨架：仅装配配置和日志，业务模块在 Phase 1+ 逐块注入。
func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	a := &App{
		cfg:    cfg,
		logger: logger,
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

// Run 启动应用（当前骨架：打印启动信息）。
// 业务服务在 Phase 1+ 加入。
func (a *App) Run(ctx context.Context) error {
	a.logger.Info("minibox 启动（骨架）",
		"port", a.cfg.Server.Port,
		"listen", a.cfg.Server.Listen,
	)
	<-ctx.Done()
	return nil
}

// Close 优雅关闭。
func (a *App) Close() error {
	a.logger.Info("minibox 关闭")
	return nil
}
