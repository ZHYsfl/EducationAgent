package tts

// SetDouBaoTTSEndpointForTest replaces the Doubao TTS WebSocket endpoint; call restore() to undo.
func SetDouBaoTTSEndpointForTest(url string) (restore func()) {
	old := doubaoTTSEndpoint
	doubaoTTSEndpoint = url
	return func() { doubaoTTSEndpoint = old }
}
