package asr

// SetDouBaoASRWebSocketURLForTest replaces the Doubao ASR WebSocket endpoint; call restore() to undo.
func SetDouBaoASRWebSocketURLForTest(url string) (restore func()) {
	old := doubaoASREndpoint
	doubaoASREndpoint = url
	return func() { doubaoASREndpoint = old }
}
