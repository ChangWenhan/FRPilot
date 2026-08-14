package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"frpmon/internal/actions"
	"frpmon/internal/ai"
	"frpmon/internal/auth"
	"frpmon/internal/cli"
	"frpmon/internal/collector"
	"frpmon/internal/config"
	"frpmon/internal/registry"
	"frpmon/internal/store"
	"frpmon/internal/traffic"
	"frpmon/internal/web"
)

const version = "0.2.0"

func main() {
	// 提供给 CLI 使用（frpm version / 部署脚本读取）
	cli.Version = version
	// GC 调优：设置堆内存软上限，避免空闲时内存膨胀；环境变量可覆盖
	if v := os.Getenv("GOMEMLIMIT"); v == "" {
		debug.SetMemoryLimit(512 << 20) // 512MB 软上限
	}
	if v := os.Getenv("GOGC"); v == "" {
		debug.SetGCPercent(100) // 显式默认
	}
	args := os.Args[1:]
	if len(args) > 0 && args[0] != "server" {
		os.Exit(cli.Run(args))
	}
	if len(args) > 0 && args[0] == "server" {
		args = args[1:]
	}
	if err := runServer(args); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}

func dataDirFlag(args []string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--data-dir" {
			return args[i+1]
		}
	}
	if v := os.Getenv("FRPMON_DATA_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/var/lib/frpilot"
	}
	return filepath.Join(home, ".frpmon")
}

func configPathFlag(args []string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--config" {
			return args[i+1]
		}
	}
	return ""
}

func runServer(args []string) error {
	// server --help / -h 时打印用法而非启动
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Println("FRPilot server [--data-dir DIR] [--config PATH] [--listen ADDR]")
			return nil
		}
	}
	dataDir := dataDirFlag(args)
	configPath := configPathFlag(args)

	cfg, err := config.LoadOrCreateAt(dataDir, configPath)
	if err != nil {
		return err
	}
	db, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	// 首次启动时兼容读取旧 config.json 中的明文敏感字段，并立即迁移到
	// SQLite 的 AES-GCM settings；之后热保存不会再把它们写回配置文件。
	if err := config.SyncEncryptedSecrets(cfg, db); err != nil {
		return err
	}

	authSvc := auth.NewService(db)
	reg := registry.NewService(db, cfg)
	col := collector.New(db, cfg)
	col.Start()
	defer col.Stop()
	traff := traffic.New(db, cfg)
	traff.Start()
	tasks := actions.NewTaskManager(db, cfg)
	aiSvc := ai.New(db, cfg)
	srv := web.NewServer(db, authSvc, cfg, reg, col, traff, tasks, aiSvc)

	addr := cfg.Get().ListenAddr
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--listen" {
			addr = args[i+1]
		}
	}
	if addr == "" {
		addr = "0.0.0.0:8443"
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// WriteTimeout 需覆盖 AI 诊断等长请求（最长 90s）
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 后台维护任务：过期会话 + 历史数据保留清理
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			db.DeleteExpiredSessions()
			db.DeleteExpiredLoginLimits()
		}
	}()
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := db.CleanupRetention(); err != nil {
				log.Printf("历史数据清理失败: %v", err)
			}
		}
	}()

	go func() {
		tlsCfg := cfg.Get().TLS
		scheme := "http"
		if tlsCfg.Enabled {
			if tlsCfg.Cert == "" || tlsCfg.Key == "" {
				log.Fatalf("TLS 已启用但未配置证书路径（tls.cert / tls.key）")
			}
			scheme = "https"
		}
		log.Printf("FRPilot v%s 启动: %s://%s (数据目录 %s)", version, scheme, addr, dataDir)
		var err error
		if tlsCfg.Enabled {
			err = httpSrv.ListenAndServeTLS(tlsCfg.Cert, tlsCfg.Key)
		} else {
			err = httpSrv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务异常: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("正在关闭...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpSrv.Shutdown(ctx)
}
