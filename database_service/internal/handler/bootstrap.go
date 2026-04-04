package handler

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"educationagent/database_service_go/internal/dbpg"
	"educationagent/database_service_go/internal/dbredis"
	"educationagent/database_service_go/internal/dbservice"
)

// Run 统一启动入口：main.go 只负责调用本函数。
func Run() error {
	host := strings.TrimSpace(os.Getenv("DATABASE_SERVICE_HOST"))
	if host == "" {
		host = "0.0.0.0"
	}
	port := 9500
	if p := strings.TrimSpace(os.Getenv("DATABASE_SERVICE_PORT")); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			port = v
		}
	}

	var repo dbservice.Store
	if dbservice.DBEnabled() {
		backend := strings.ToLower(strings.TrimSpace(os.Getenv("PPT_DATABASE_BACKEND")))
		if backend == "" {
			backend = "postgres"
		}
		switch backend {
		case "postgres", "postgresql", "pg":
			r, err := dbpg.Connect(context.Background())
			if err != nil {
				log.Printf("database-service: PostgreSQL 连接失败: %v", err)
			} else {
				repo = r
				defer r.Close()
			}
		default:
			r, err := dbredis.Connect(context.Background())
			if err != nil {
				log.Printf("database-service: Redis 连接失败: %v", err)
			} else {
				repo = r
				defer r.Close()
			}
		}
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(dbservice.AuthMiddleware)

	srv := &dbservice.Server{R: repo}
	registerRoutes(r, srv)

	addr := host + ":" + strconv.Itoa(port)
	log.Printf("database-service listening on %s", addr)
	return http.ListenAndServe(addr, r)
}

