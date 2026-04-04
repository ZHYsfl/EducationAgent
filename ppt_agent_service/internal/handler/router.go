package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"educationagent/ppt_agent_service_go/internal/api"
	"educationagent/ppt_agent_service_go/internal/ecode"
	"educationagent/ppt_agent_service_go/internal/pptserver"
)

func buildRouter(srv *pptserver.Server) http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", srv.HandleHealthz)
	fs := http.FileServer(http.Dir(srv.RunsAbs))
	r.Handle("/static/runs/*", http.StripPrefix("/static/runs/", fs))

	r.Route("/", func(r chi.Router) {
		r.Use(srv.AuthMiddleware())
		r.Post("/api/v1/ppt/init", srv.HandlePPTInit)
		r.Post("/api/v1/ppt/vad", srv.HandleVAD)
		r.Post("/api/v1/canvas/vad-event", srv.HandleVAD)
		r.Post("/api/v1/ppt/feedback", srv.HandleFeedback)
		r.Get("/api/v1/canvas/status", srv.HandleCanvasStatus)
		r.Get("/api/v1/tasks/{taskID}/preview", srv.HandleTaskPreview)
		r.Post("/api/v1/tasks", srv.HandleCreateTask)
		r.Get("/api/v1/tasks/{taskID}", srv.HandleGetTaskDetail)
		r.Put("/api/v1/tasks/{taskID}/status", srv.HandleUpdateTaskStatus)
		r.Get("/api/v1/tasks", srv.HandleListTasks)
		r.Post("/api/v1/ppt/export", srv.HandlePPTExport)
		r.Get("/api/v1/ppt/export/{exportID}", srv.HandleGetExport)
		r.Get("/api/v1/ppt/page/{pageID}/render", srv.HandlePageRender)
		r.Post("/api/v1/ppt/canvas/render/execute", srv.HandleRenderExecute)
		r.Post("/api/v1/kb/parse", srv.HandleKBParse)
		r.Post("/api/v1/sessions", srv.HandleProxyDB)
		r.Get("/api/v1/sessions/{sessionID}", srv.HandleProxyDB)
		r.Get("/api/v1/sessions", srv.HandleProxyDB)
		r.Put("/api/v1/sessions/{sessionID}", srv.HandleProxyDB)
		r.Patch("/api/v1/sessions/{sessionID}", srv.HandleProxyDB)
	})
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		api.WriteErr(w, ecode.NotFound, "资源不存在")
	})
	return r
}

