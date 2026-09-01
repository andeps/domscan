package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"domscan/availability"
	"domscan/config"
	"domscan/httpserver"
	"domscan/notification"
	"domscan/rediscache"
	"domscan/user"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.Load(".env")
	if err != nil {
		log.Fatalf("加载配置失败：%v", err)
	}
	addr := flag.String("addr", cfg.Address, "HTTP listen address (overrides .env)")
	flag.Parse()
	var sources []availability.Checker
	bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), cfg.RDAPTimeout)
	ianaChecker, bootstrapErr := availability.NewIANAChecker(bootstrapCtx, cfg.RDAPBootstrapURL, cfg.RDAPTimeout, cfg.RDAPProviderConcurrency)
	bootstrapCancel()
	if bootstrapErr != nil {
		log.Printf("IANA RDAP 路由表加载失败，将使用备用检测源：%v", bootstrapErr)
	} else {
		sources = append(sources, ianaChecker)
		log.Printf("IANA 权威 RDAP 路由已启用")
	}
	for _, fallbackURL := range cfg.RDAPFallbackURLs {
		sources = append(sources, availability.NewLimitedRDAPChecker(fallbackURL, cfg.RDAPTimeout, cfg.RDAPProviderConcurrency))
	}
	var checker availability.Checker = availability.NewFallbackChecker(sources...)
	cacheEnabled := false
	if cfg.RedisEnabled {
		cache, cacheErr := rediscache.New(cfg.RedisURL, cfg.RedisKeyPrefix, cfg.RedisDialTimeout)
		if cacheErr != nil {
			log.Fatalf("Redis 配置无效：%v", cacheErr)
		}
		pingCtx, pingCancel := context.WithTimeout(context.Background(), cfg.RedisDialTimeout)
		pingErr := cache.Ping(pingCtx)
		pingCancel()
		if pingErr != nil {
			log.Printf("Redis 不可用，已降级为直接查询 RDAP：%v", pingErr)
			_ = cache.Close()
		} else {
			defer cache.Close()
			checker = availability.NewCachedChecker(checker, cache, cfg.CacheTTL, cfg.CacheUnknownTTL)
			cacheEnabled = true
			log.Printf("Redis 域名缓存已启用，正常结果 TTL=%s，未知结果 TTL=%s", cfg.CacheTTL, cfg.CacheUnknownTTL)
		}
	}
	var userHandler *user.Handler
	if cfg.DatabaseEnabled {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		db, dbErr := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{
			DisableAutomaticPing: true,
			TranslateError:       true,
		})
		var sqlDB *sql.DB
		if dbErr == nil {
			sqlDB, dbErr = db.DB()
		}
		if dbErr == nil {
			dbErr = sqlDB.PingContext(ctx)
		}
		cancel()
		if dbErr != nil {
			log.Printf("PostgreSQL 不可用，用户模块已禁用：%v", dbErr)
			if sqlDB != nil {
				_ = sqlDB.Close()
			}
		} else {
			repo := user.NewRepository(db)
			if dbErr = repo.Migrate(context.Background()); dbErr != nil {
				log.Printf("用户表初始化失败：%v", dbErr)
				_ = sqlDB.Close()
			} else {
				defer sqlDB.Close()
				userHandler = user.NewHandler(repo, cfg.JWTSecret)
				log.Printf("PostgreSQL 用户模块已通过 GORM 启用")
			}
		}
	}
	var notifier httpserver.Notifier
	if cfg.EmailEnabled {
		if userHandler == nil {
			log.Printf("用户模块不可用，邮箱通知已禁用")
		} else {
			notifier = notification.NewSMTPNotifier(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.EmailFrom, cfg.EmailTimeout)
			log.Printf("邮箱通知已启用，扫描结果将发送给当前登录用户")
		}
	}
	handler := httpserver.New(checker, httpserver.Options{GinMode: cfg.GinMode, RequestTimeout: cfg.RequestTimeout, CacheEnabled: cacheEnabled, Notifier: notifier, NotificationBatchSize: cfg.EmailBatchSize, UserHandler: userHandler})
	server := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: cfg.ReadHeaderTimeout, IdleTimeout: cfg.IdleTimeout}
	go func() {
		log.Printf("域名检测工具已启动：http://localhost%s", displayAddr(*addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("启动失败：%v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("关闭服务失败：%v", err)
	}
}

func displayAddr(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return addr
	}
	return fmt.Sprintf(" (%s)", addr)
}
