package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Black0Bag/minibox/internal/api"
	"github.com/Black0Bag/minibox/internal/config"
	"github.com/Black0Bag/minibox/internal/fsutil"
	"github.com/Black0Bag/minibox/internal/logging"
	"github.com/Black0Bag/minibox/internal/timestamp"
)

// Version 版本号单一来源（N-14）：构建时以 -ldflags "-X main.Version=vX.Y.Z" 注入
var Version = "dev"

var (
	configPath   string
	portOverride int
	dataDir      string
)

var rootCmd = &cobra.Command{
	Use:     "minibox",
	Short:   "minibox 中枢大脑：私有化强 Agent 后端",
	Long:    "minibox 是一个以 Go 后端为中枢大脑、以安卓 APP 为眼耳口手的私有化强 Agent 系统。",
	Version: Version,
}

var startCmd = &cobra.Command{
	Use:   "启动",
	Short: "启动 HTTP API 服务",
	RunE:  runStart,
}

var initCmd = &cobra.Command{
	Use:   "配置",
	Short: "生成配置文件（首次启动向导）",
	RunE:  runInit,
}

var statusCmd = &cobra.Command{
	Use:   "状态",
	Short: "检查服务状态",
	RunE:  runStatus,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "config.yaml", "配置文件路径")
	rootCmd.PersistentFlags().IntVar(&portOverride, "port", 0, "覆盖监听端口（优先级高于配置文件，N-14）")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "覆盖数据目录")
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(statusCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// loadEffectiveConfig 加载配置并应用命令行覆盖
func loadEffectiveConfig() (*config.Config, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if portOverride > 0 {
		cfg.Server.Port = portOverride
	}
	if dataDir != "" {
		cfg.DataDir = dataDir
	}
	return cfg, nil
}

// runStart 启动服务
func runStart(cmd *cobra.Command, args []string) error {
	cfg, err := loadEffectiveConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 确保数据目录
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("创建数据目录 %s: %w", cfg.DataDir, err)
	}

	// 初始化日志（默认写入 data/logs/minibox.log）
	lg := cfg.Logging
	if lg.File == "" {
		lg.File = filepath.Join(cfg.DataDir, "logs", "minibox.log")
	}
	if err := os.MkdirAll(filepath.Dir(lg.File), 0o755); err != nil {
		return fmt.Errorf("创建日志目录: %w", err)
	}
	logger, err := logging.Init(lg)
	if err != nil {
		return fmt.Errorf("初始化日志失败: %w", err)
	}

	// 初始化统一时间服务（NTP 校准，断网回退系统时钟）
	ts := timestamp.New("")
	_ = ts.Sync() // 失败不阻塞
	stopSync := make(chan struct{})
	ts.StartPeriodicSync(time.Hour, stopSync)
	defer close(stopSync)

	// 初始化全局序号（Phase 0.4）
	seq, err := fsutil.NewSeqStore(filepath.Join(cfg.DataDir, "seq"))
	if err != nil {
		return fmt.Errorf("初始化序号存储失败: %w", err)
	}

	// 初始化 fsutil + logme（Phase 0）
	lm := fsutil.NewLogMe(cfg.DataDir, seq, ts)
	files := fsutil.NewFS(lm, ts)
	if err := lm.Ensure(); err != nil {
		logger.Warn("logme 初始化警告", "err", err)
	}
	_ = files

	// 构建 API 服务
	apiSrv := api.NewServer(cfg, logger, Version)
	httpSrv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           apiSrv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      250 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 优雅关停（N-01）：SIGTERM/SIGINT → 30s 内排空请求
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("minibox 服务启动",
			"version", Version,
			"addr", cfg.Addr(),
			"data_dir", cfg.DataDir,
			"log_level", logging.CurrentLevel(),
		)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP 服务错误: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("收到关闭信号，开始优雅关停（30 秒超时）")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("优雅关停超时", "err", err)
	}
	logger.Info("服务已退出")
	return nil
}

// runInit 生成配置文件
func runInit(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("配置文件 %s 已存在，拒绝覆盖（只允许追加，不删改）", configPath)
	}
	cfg := config.Default()
	if err := cfg.Save(configPath); err != nil {
		return err
	}
	fmt.Printf("已生成配置文件 %s（权限 0600）。请编辑 providers 填入你的 LLM 供应商。\n", configPath)
	return nil
}

// runStatus 检查状态
func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := loadEffectiveConfig()
	if err != nil {
		return err
	}
	fmt.Printf("minibox %s\n", Version)
	fmt.Printf("  配置文件: %s\n", configPath)
	fmt.Printf("  监听地址: %s\n", cfg.Addr())
	fmt.Printf("  数据目录: %s\n", cfg.DataDir)
	fmt.Printf("  供应商数: %d\n", len(cfg.Providers))
	fmt.Println("  状态: 未启动（运行 `minibox 启动` 启动服务）")
	return nil
}
