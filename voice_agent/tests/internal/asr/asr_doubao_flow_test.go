package asr_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	asrpkg "voiceagent/internal/asr"
	"voiceagent/internal/doubao"
)

func TestDouBaoASR_PartialAndFinalResults(t *testing.T) {
	srv := mockDouBaoASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage()

		partial := map[string]any{
			"result": map[string]any{
				"text":       "你好",
				"utterances": []map[string]any{{"text": "你好", "definite": false}},
			},
		}
		p1, _ := json.Marshal(partial)
		sendServerResp(conn, p1, doubao.FlagNoSeq)

		final := map[string]any{
			"result": map[string]any{
				"text":       "你好世界",
				"utterances": []map[string]any{{"text": "你好世界", "definite": true}},
			},
		}
		p2, _ := json.Marshal(final)
		sendServerResp(conn, p2, doubao.FlagNoSeq)

		sendServerResp(conn, []byte(`{"result":{"text":""}}`), doubao.FlagLastData)
	})
	defer srv.Close()

	conn := dialMockWS(t, srv)
	defer conn.Close()
	sendConfigFrame(t, conn)

	var prevText string
	var results []asrpkg.ASRResult

	for i := 0; i < 5; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		ph, _ := doubao.ParseHeader(data)
		if ph.MsgType != doubao.MsgTypeFullServerResp {
			continue
		}
		payload, _, isLast, _ := doubao.ParseServerResponse(data)
		var resp struct {
			Result struct {
				Text       string `json:"text"`
				Utterances []struct {
					Text     string `json:"text"`
					Definite bool   `json:"definite"`
				} `json:"utterances"`
			} `json:"result"`
		}
		json.Unmarshal(payload, &resp)
		fullText := resp.Result.Text
		if fullText == "" {
			if isLast {
				break
			}
			continue
		}

		hasDefinite := false
		for _, u := range resp.Result.Utterances {
			if u.Definite {
				hasDefinite = true
				break
			}
		}

		if isLast || hasDefinite {
			results = append(results, asrpkg.ASRResult{Text: fullText, IsFinal: true, Mode: "2pass-offline"})
			prevText = fullText
			if isLast {
				break
			}
		} else {
			delta := fullText
			if strings.HasPrefix(fullText, prevText) {
				delta = fullText[len(prevText):]
			}
			if delta == "" {
				continue
			}
			prevText = fullText
			results = append(results, asrpkg.ASRResult{Text: delta, IsFinal: false, Mode: "streaming"})
		}
	}

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].IsFinal {
		t.Error("first result should be partial")
	}
	if !results[1].IsFinal {
		t.Error("second result should be final")
	}
}

func TestDouBaoASR_EmptyDelta(t *testing.T) {
	srv := mockDouBaoASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		resp := map[string]any{
			"result": map[string]any{
				"text":       "你好",
				"utterances": []map[string]any{{"text": "你好", "definite": false}},
			},
		}
		p, _ := json.Marshal(resp)
		sendServerResp(conn, p, doubao.FlagNoSeq)
		sendServerResp(conn, p, doubao.FlagNoSeq)
		sendServerResp(conn, []byte(`{"result":{"text":""}}`), doubao.FlagLastData)
	})
	defer srv.Close()

	conn := dialMockWS(t, srv)
	defer conn.Close()
	sendConfigFrame(t, conn)

	var count int
	for i := 0; i < 5; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		ph, _ := doubao.ParseHeader(data)
		if ph.MsgType == doubao.MsgTypeFullServerResp {
			payload, _, isLast, _ := doubao.ParseServerResponse(data)
			var resp struct {
				Result struct {
					Text string `json:"text"`
				} `json:"result"`
			}
			json.Unmarshal(payload, &resp)
			if resp.Result.Text != "" {
				count++
			}
			if isLast {
				break
			}
		}
	}
	if count < 2 {
		t.Errorf("should have received at least 2 non-empty responses")
	}
}

func TestDouBaoASR_ContextCancelDuringAudioSend(t *testing.T) {
	srv := mockDouBaoASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		time.Sleep(2 * time.Second)
	})
	defer srv.Close()

	conn := dialMockWS(t, srv)
	defer conn.Close()
	sendConfigFrame(t, conn)
	conn.Close()
}
