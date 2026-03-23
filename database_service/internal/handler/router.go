package handler

import (
	"github.com/go-chi/chi/v5"

	"educationagent/database_service_go/internal/dbservice"
)

func registerRoutes(r chi.Router, srv *dbservice.Server) {
	r.Get("/healthz", srv.HandleHealthz)
	r.Get("/", srv.HandleRoot)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/sessions", srv.HandleCreateSession)
		r.Get("/sessions/{sessionID}", srv.HandleGetSession)
		r.Get("/sessions", srv.HandleListSessions)
		r.Put("/sessions/{sessionID}", srv.HandleUpdateSession)
		r.Post("/tasks", srv.HandleCreateTask)
		r.Get("/tasks/{taskID}", srv.HandleGetTask)
		r.Put("/tasks/{taskID}/status", srv.HandleUpdateTaskStatus)
		r.Get("/tasks", srv.HandleListTasks)
		r.Post("/files/upload", srv.HandleUploadFile)
		r.Get("/files/{fileID}", srv.HandleGetFile)
		r.Delete("/files/{fileID}", srv.HandleDeleteFile)
	})

	r.Route("/internal/db", func(r chi.Router) {
		r.Post("/ensure-user", srv.HandleInternalEnsureUser)
		r.Post("/ensure-session", srv.HandleInternalEnsureSession)
		r.Post("/create-session", srv.HandleInternalCreateSession)
		r.Get("/sessions/{sessionID}", srv.HandleInternalGetSession)
		r.Get("/sessions", srv.HandleInternalListSessions)
		r.Patch("/sessions/{sessionID}", srv.HandleInternalPatchSession)
		r.Put("/tasks/{taskID}", srv.HandleInternalPutTask)
		r.Get("/tasks/{taskID}", srv.HandleInternalGetTask)
		r.Get("/task-list", srv.HandleInternalTaskList)
		r.Put("/exports/{exportID}", srv.HandleInternalPutExport)
		r.Get("/exports/{exportID}", srv.HandleInternalGetExport)
	})
}

