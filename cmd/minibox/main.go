// minibox 后端入口。
// thin main：只调用 run()，不包含业务逻辑。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Black0Bag/minibox/internal/app"
	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/platform/logging"
)

// 版本信息（GoReleaser ldflags 注入：-X main.version=...）。
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "minibox 启动失败: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// 解析命令行参数
	var (
		configPath = flag.String("config", "", "配置文件路径（可选）")
		showVer    = flag.Bool("version", false, "显示版本")
		initMode   = flag.Bool("init", false, "首次初始化：跳过交互向导，直接完成配置并启动")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("minibox %s (commit %s, built %s)\n", version, commit, date)
		return nil
	}

	// --init 模式：快速完成首次配置向导，不启动服务
	if *initMode {
		cfg, err := config.Load(*configPath)
		if err != nil {
			return fmt.Errorf("加载配置: %w", err)
		}
		logger, err := logging.New(cfg.Logging)
		if err != nil {
			return fmt.Errorf("初始化日志: %w", err)
		}
		slog.SetDefault(logger)

		ctx := context.Background()
		application, err := app.New(ctx, cfg, logger)
		if err != nil {
			return fmt.Errorf("装配应用: %w", err)
		}
		defer application.Close()

		wz := application.Wizard()
		need, _ := wz.NeedWizard()
		if !need {
			fmt.Println("✅ 初始化已完成，无需重复执行。")
			return nil
		}
		if _, err := wz.Complete(); err != nil {
			return fmt.Errorf("完成向导: %w", err)
		}
		fmt.Println("✅ minibox 初始化完成！")
		fmt.Printf("   配置文件: %s\n", *configPath)
		fmt.Println("   现在可以运行 'minibox' 启动服务。")
		return nil
	}

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("加载配置: %w", err)
	}

	// 初始化日志
	logger, err := logging.New(cfg.Logging)
	if err != nil {
		return fmt.Errorf("初始化日志: %w", err)
	}
	slog.SetDefault(logger)

	// 上下文 + 优雅停机
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 装配应用
	application, err := app.New(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("装配应用: %w", err)
	}

	// 运行
	if err := application.Run(ctx); err != nil {
		return err
	}
	return application.Close()
}