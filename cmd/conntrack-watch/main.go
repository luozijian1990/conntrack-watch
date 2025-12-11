package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"conntrack-watch-new/internal/config"
	"conntrack-watch-new/internal/conntrack"
	"conntrack-watch-new/internal/logger"
	"conntrack-watch-new/web"
)

func main() {
	// 命令行参数
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化日志
	logger.Init(logger.Config{
		Path:       cfg.Log.Path,
		MaxSizeMB:  cfg.Log.MaxSizeMB,
		MaxBackups: cfg.Log.MaxBackups,
		MaxAgeDays: cfg.Log.MaxAgeDays,
		Compress:   cfg.Log.Compress,
	})
	defer logger.Log.Sync()

	logger.Log.Info("程序启动，监控端口配置完成")

	// 创建连接监控器
	watcher, err := conntrack.NewWatcher(cfg.Ports)
	if err != nil {
		logger.Log.Error("创建 conntrack 监控器失败: " + err.Error())
		os.Exit(1)
	}
	defer watcher.Close()

	// 启动 Web 服务（包含 Prometheus 和查询 API）
	if cfg.Prometheus.Enabled {
		server := web.NewServer(watcher)
		server.Start(cfg.Prometheus.ListenAddr, cfg.WebUI.Enabled)
	}

	// 开始监控
	ctx := context.Background()
	if err := watcher.Start(ctx); err != nil {
		logger.Log.Error("启动监控失败: " + err.Error())
		os.Exit(1)
	}

	logger.Log.Info("开始监听 conntrack 事件...")

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Log.Info("收到退出信号，关闭...")
}
