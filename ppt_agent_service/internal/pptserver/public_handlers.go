package pptserver

import (
	"net/http"

	"educationagent/ppt_agent_service_go/internal/auth"
)

// Exported handler wrappers for router composition in internal/handler.
func (s *Server) HandleHealthz(w http.ResponseWriter, r *http.Request)        { s.healthz(w, r) }
func (s *Server) HandlePPTInit(w http.ResponseWriter, r *http.Request)        { s.pptInit(w, r) }
func (s *Server) HandleVAD(w http.ResponseWriter, r *http.Request)            { s.vad(w, r) }
func (s *Server) HandleFeedback(w http.ResponseWriter, r *http.Request)       { s.feedback(w, r) }
func (s *Server) HandleCanvasStatus(w http.ResponseWriter, r *http.Request)   { s.canvasStatus(w, r) }
func (s *Server) HandleTaskPreview(w http.ResponseWriter, r *http.Request)    { s.taskPreview(w, r) }
func (s *Server) HandleCreateTask(w http.ResponseWriter, r *http.Request)     { s.createTask(w, r) }
func (s *Server) HandleGetTaskDetail(w http.ResponseWriter, r *http.Request)  { s.getTaskDetail(w, r) }
func (s *Server) HandleUpdateTaskStatus(w http.ResponseWriter, r *http.Request) {
	s.updateTaskStatus(w, r)
}
func (s *Server) HandleListTasks(w http.ResponseWriter, r *http.Request)          { s.listTasks(w, r) }
func (s *Server) HandlePPTExport(w http.ResponseWriter, r *http.Request)          { s.pptExport(w, r) }
func (s *Server) HandleGetExport(w http.ResponseWriter, r *http.Request)          { s.getExport(w, r) }
func (s *Server) HandlePageRender(w http.ResponseWriter, r *http.Request)         { s.pageRender(w, r) }
func (s *Server) HandleRenderExecute(w http.ResponseWriter, r *http.Request)      { s.renderExecute(w, r) }
func (s *Server) HandleKBParse(w http.ResponseWriter, r *http.Request)            { s.kbParse(w, r) }
func (s *Server) HandleProxyDB(w http.ResponseWriter, r *http.Request)            { s.proxyDB(w, r) }
func (s *Server) AuthMiddleware() func(http.Handler) http.Handler                  { return auth.Middleware(s.Cfg) }

