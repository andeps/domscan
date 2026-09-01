// Package httpserver exposes the domain generator and availability checker over HTTP.
package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"domscan/availability"
	"domscan/domain"
	"domscan/user"

	"github.com/gin-gonic/gin"
)

const maxCheckDomains = 1000

type server struct {
	checker      availability.Checker
	cacheEnabled bool
	notifier     Notifier
	notifyBatch  int
	userHandler  *user.Handler
}

type Notifier interface {
	Notify(context.Context, string, []availability.Result) error
}

// Options controls framework and request lifecycle behavior.
type Options struct {
	GinMode               string
	RequestTimeout        time.Duration
	CacheEnabled          bool
	Notifier              Notifier
	NotificationBatchSize int
	UserHandler           *user.Handler
}

// New builds the Gin API application.
func New(checker availability.Checker, supplied ...Options) http.Handler {
	options := Options{GinMode: gin.ReleaseMode, RequestTimeout: 15 * time.Minute}
	if len(supplied) > 0 {
		options = supplied[0]
	}
	if options.GinMode == "" {
		options.GinMode = gin.ReleaseMode
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = 15 * time.Minute
	}
	gin.SetMode(options.GinMode)

	batchSize := options.NotificationBatchSize
	if batchSize < 1 {
		batchSize = 10
	}
	app := &server{checker: checker, cacheEnabled: options.CacheEnabled, notifier: options.Notifier, notifyBatch: batchSize}
	app.userHandler = options.UserHandler
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.Use(gin.Logger(), gin.Recovery(), apiHeaders(), requestTimeout(options.RequestTimeout))
	if err := router.SetTrustedProxies(nil); err != nil {
		panic(err)
	}

	router.GET("/api/health", app.handleHealth)
	router.POST("/api/generate", app.handleGenerate)
	router.POST("/api/check", app.handleCheck)
	if app.userHandler != nil {
		router.POST("/api/search", app.userHandler.AuthRequired(), app.handleSearch)
	} else {
		router.POST("/api/search", app.handleSearch)
	}
	if app.userHandler != nil {
		router.POST("/api/auth/register", app.userHandler.Register)
		router.POST("/api/auth/login", app.userHandler.Login)
		auth := app.userHandler.AuthRequired()
		router.PUT("/api/auth/email", auth, app.userHandler.UpdateEmail)
		router.PUT("/api/auth/password", auth, app.userHandler.UpdatePassword)
		router.PUT("/api/auth/avatar", auth, app.userHandler.UpdateAvatar)
	}
	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
	})
	router.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "请求方法不支持"})
	})
	return router
}

func (s *server) handleHealth(c *gin.Context) {
	cacheStatus := "disabled"
	if s.cacheEnabled {
		cacheStatus = "redis"
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "framework": "gin", "cache": cacheStatus})
}

func (s *server) handleGenerate(c *gin.Context) {
	var opts domain.Options
	if err := decodeJSON(c, &opts); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	domains, err := domain.Generate(opts)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"domains": domains, "count": len(domains)})
}

type checkRequest struct {
	Domains     []string `json:"domains"`
	Concurrency int      `json:"concurrency"`
}

type searchRequest struct {
	Options     domain.Options `json:"options"`
	Concurrency int            `json:"concurrency"`
}

type searchSummary struct {
	Event              string `json:"event"`
	Fresh              int    `json:"fresh"`
	CachedSkipped      int    `json:"cachedSkipped"`
	Generated          int    `json:"generated"`
	NotificationQueued int    `json:"notificationQueued,omitempty"`
}

type ndjsonStream struct {
	encoder    *json.Encoder
	controller *http.ResponseController
}

func newNDJSONStream(c *gin.Context) *ndjsonStream {
	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Header("Cache-Control", "no-store, no-transform")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.WriteHeaderNow()
	controller := http.NewResponseController(c.Writer)
	_ = controller.Flush()
	return &ndjsonStream{encoder: json.NewEncoder(c.Writer), controller: controller}
}

func (s *ndjsonStream) Write(value any) error {
	if err := s.encoder.Encode(value); err != nil {
		return err
	}
	return s.controller.Flush()
}

func (s *server) handleCheck(c *gin.Context) {
	var input checkRequest
	if err := decodeJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(input.Domains) == 0 || len(input.Domains) > maxCheckDomains {
		c.JSON(http.StatusBadRequest, gin.H{"error": "每次可检测 1 到 1000 个域名"})
		return
	}
	for _, candidate := range input.Domains {
		if !domain.Valid(candidate) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("域名 %q 无效", candidate)})
			return
		}
	}
	if input.Concurrency <= 0 {
		input.Concurrency = min(8, runtime.GOMAXPROCS(0)*2)
	}
	if input.Concurrency > 20 {
		input.Concurrency = 20
	}

	stream := newNDJSONStream(c)
	for result := range s.checkAll(c.Request.Context(), input.Domains, input.Concurrency) {
		if err := stream.Write(result); err != nil {
			return
		}
	}
}

func (s *server) handleSearch(c *gin.Context) {
	var input searchRequest
	if err := decodeJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.Options.Limit <= 0 {
		input.Options.Limit = 200
	}
	if input.Options.Limit > maxCheckDomains {
		input.Options.Limit = maxCheckDomains
	}
	if input.Concurrency <= 0 {
		input.Concurrency = min(8, runtime.GOMAXPROCS(0)*2)
	}
	if input.Concurrency > 20 {
		input.Concurrency = 20
	}
	if _, err := domain.Generate(input.Options); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	stream := newNDJSONStream(c)
	target, offset := input.Options.Limit, input.Options.Offset
	fresh, skipped, generated := 0, 0, 0
	notificationQueued := 0
	recipient := user.EmailFromContext(c)
	pendingNotifications := make([]availability.Result, 0, s.notifyBatch)
	const maxBatches = 100
	for batch := 0; batch < maxBatches && fresh < target; batch++ {
		batchOptions := input.Options
		batchOptions.Offset = offset
		batchOptions.Limit = min(target-fresh, maxCheckDomains)
		candidates, err := domain.Generate(batchOptions)
		if err != nil || len(candidates) == 0 {
			break
		}
		generated += len(candidates)
		for result := range s.checkAll(c.Request.Context(), candidates, input.Concurrency) {
			if result.Cached {
				skipped++
				continue
			}
			if fresh >= target {
				continue
			}
			if err := stream.Write(result); err != nil {
				return
			}
			fresh++
			if result.Status == availability.StatusAvailable {
				if s.notifier != nil && recipient != "" {
					notificationQueued++
					pendingNotifications = append(pendingNotifications, result)
					if len(pendingNotifications) >= s.notifyBatch {
						s.notifyAsync(recipient, pendingNotifications)
						pendingNotifications = make([]availability.Result, 0, s.notifyBatch)
					}
				}
			}
		}
		offset += len(candidates)
		if len(candidates) < batchOptions.Limit {
			break
		}
	}
	if len(pendingNotifications) > 0 && c.Request.Context().Err() == nil {
		s.notifyAsync(recipient, pendingNotifications)
	}
	summary := searchSummary{Event: "summary", Fresh: fresh, CachedSkipped: skipped, Generated: generated, NotificationQueued: notificationQueued}
	_ = stream.Write(summary)
}

func (s *server) notifyAsync(recipient string, results []availability.Result) {
	batch := append([]availability.Result(nil), results...)
	go func() {
		_ = s.notifier.Notify(context.Background(), recipient, batch)
	}()
}

func (s *server) checkAll(ctx context.Context, candidates []string, concurrency int) <-chan availability.Result {
	jobs := make(chan string)
	results := make(chan availability.Result)
	var workers sync.WaitGroup
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for candidate := range jobs {
				select {
				case results <- s.checker.Check(ctx, candidate):
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, candidate := range candidates {
			select {
			case jobs <- candidate:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	return results
}

func decodeJSON(c *gin.Context, target any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func apiHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	}
}

func requestTimeout(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
