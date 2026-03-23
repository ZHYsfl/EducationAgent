package dbservice

import "net/http"

// Exported handler wrappers for external router composition.
func (s *Server) HandleHealthz(w http.ResponseWriter, r *http.Request)            { s.healthz(w, r) }
func (s *Server) HandleRoot(w http.ResponseWriter, r *http.Request)               { s.root(w, r) }
func (s *Server) HandleCreateSession(w http.ResponseWriter, r *http.Request)      { s.createSession(w, r) }
func (s *Server) HandleGetSession(w http.ResponseWriter, r *http.Request)         { s.getSession(w, r) }
func (s *Server) HandleListSessions(w http.ResponseWriter, r *http.Request)       { s.listSessions(w, r) }
func (s *Server) HandleUpdateSession(w http.ResponseWriter, r *http.Request)      { s.updateSession(w, r) }
func (s *Server) HandleCreateTask(w http.ResponseWriter, r *http.Request)         { s.createTask(w, r) }
func (s *Server) HandleGetTask(w http.ResponseWriter, r *http.Request)            { s.getTask(w, r) }
func (s *Server) HandleUpdateTaskStatus(w http.ResponseWriter, r *http.Request)   { s.updateTaskStatus(w, r) }
func (s *Server) HandleListTasks(w http.ResponseWriter, r *http.Request)          { s.listTasks(w, r) }
func (s *Server) HandleUploadFile(w http.ResponseWriter, r *http.Request)         { s.uploadFile(w, r) }
func (s *Server) HandleGetFile(w http.ResponseWriter, r *http.Request)            { s.getFile(w, r) }
func (s *Server) HandleDeleteFile(w http.ResponseWriter, r *http.Request)         { s.deleteFile(w, r) }
func (s *Server) HandleInternalEnsureUser(w http.ResponseWriter, r *http.Request) { s.internalEnsureUser(w, r) }
func (s *Server) HandleInternalEnsureSession(w http.ResponseWriter, r *http.Request) {
	s.internalEnsureSession(w, r)
}
func (s *Server) HandleInternalCreateSession(w http.ResponseWriter, r *http.Request) {
	s.internalCreateSession(w, r)
}
func (s *Server) HandleInternalGetSession(w http.ResponseWriter, r *http.Request) {
	s.internalGetSession(w, r)
}
func (s *Server) HandleInternalListSessions(w http.ResponseWriter, r *http.Request) {
	s.internalListSessions(w, r)
}
func (s *Server) HandleInternalPatchSession(w http.ResponseWriter, r *http.Request) {
	s.internalPatchSession(w, r)
}
func (s *Server) HandleInternalPutTask(w http.ResponseWriter, r *http.Request)    { s.internalPutTask(w, r) }
func (s *Server) HandleInternalGetTask(w http.ResponseWriter, r *http.Request)    { s.internalGetTask(w, r) }
func (s *Server) HandleInternalTaskList(w http.ResponseWriter, r *http.Request)   { s.internalTaskList(w, r) }
func (s *Server) HandleInternalPutExport(w http.ResponseWriter, r *http.Request)  { s.internalPutExport(w, r) }
func (s *Server) HandleInternalGetExport(w http.ResponseWriter, r *http.Request)  { s.internalGetExport(w, r) }

