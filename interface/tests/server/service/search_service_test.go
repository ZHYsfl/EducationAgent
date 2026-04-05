package service_test

import (
	"testing"

	"multimodal-teaching-agent/internal/server"
)

func TestFetchSearchResults_Dispatch(t *testing.T) {
	t.Parallel()

	app := server.NewAppForTest(nil, nil)

	// default serpapi key empty -> should error in serpapi branch
	_, _, err := app.SearchBySerpAPI("test", 3, "zh")
	if err == nil {
		t.Fatal("expected error when SERPAPI_KEY is empty")
	}

	_, _, err = app.FetchSearchResults("test", 3, "zh")
	if err == nil {
		t.Fatal("expected dispatch to fail because provider key not configured")
	}
}
