package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"educationagent/ppt_agent_service_go/internal/config"
	"educationagent/ppt_agent_service_go/internal/pptserver"
)

// Run 统一启动入口：main.go 只保留启动调用。
func Run() error {
	cfg := config.Load()
	srv, err := pptserver.New(cfg)
	if err != nil {
		return fmt.Errorf("pptserver.New: %w", err)
	}
	defer srv.Close()

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           buildRouter(srv),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if e := httpSrv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			errCh <- e
			return
		}
		errCh <- nil
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
		return nil
	case e := <-errCh:
		return e
	}
}

