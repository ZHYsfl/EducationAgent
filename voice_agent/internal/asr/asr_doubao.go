package asr

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"voiceagent/internal/doubao"
)

var doubaoASREndpoint = "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async"

type DouBaoASRConfig struct {
	AppKey     string
	AccessKey  string
	ResourceId string
}

type DouBaoASRClient struct {
	config DouBaoASRConfig
}

func NewDouBaoASRClient(cfg DouBaoASRConfig) *DouBaoASRClient {
	return &DouBaoASRClient{config: cfg}
}

// Config returns the client configuration (for tests).
func (c *DouBaoASRClient) Config() DouBaoASRConfig { return c.config }

func (c *DouBaoASRClient) RecognizeStream(ctx context.Context, audioCh <-chan []byte, resultBufSize int) (<-chan ASRResult, error) {
	connectId := uuid.New().String()

	header := http.Header{}
	header.Set("X-Api-App-Key", c.config.AppKey)
	header.Set("X-Api-Access-Key", c.config.AccessKey)
	header.Set("X-Api-Resource-Id", c.config.ResourceId)
	header.Set("X-Api-Connect-Id", connectId)

	dialer := websocket.Dialer{}
	conn, resp, err := dialer.DialContext(ctx, doubaoASREndpoint, header)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("doubao asr connect (status %d): %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("doubao asr connect: %w", err)
	}
	if logId := resp.Header.Get("X-Tt-Logid"); logId != "" {
		log.Printf("doubao asr logid: %s", logId)
	}

	cfgPayload, err := json.Marshal(map[string]any{
		"audio": map[string]any{
			"format":  "pcm",
			"rate":    16000,
			"bits":    16,
			"channel": 1,
		},
		"request": map[string]any{
			"model_name":      "bigmodel",
			"enable_itn":      true,
			"enable_punc":     true,
			"result_type":     "full",
			"show_utterances": true,
		},
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("doubao asr config marshal: %w", err)
	}

	h := doubao.BuildHeader(doubao.MsgTypeFullClientReq, doubao.FlagNoSeq, doubao.SerJSON, doubao.CompNone)
	frame := doubao.BuildFrame(h, cfgPayload)
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		conn.Close()
		return nil, fmt.Errorf("doubao asr config send: %w", err)
	}

	resultCh := make(chan ASRResult, resultBufSize)

	// Writer goroutine: forward PCM audio from audioCh to the Doubao ASR server.
	go func() {
		audioHeader := doubao.BuildHeader(doubao.MsgTypeAudioOnlyReq, doubao.FlagNoSeq, doubao.SerNone, doubao.CompNone)
		lastHeader := doubao.BuildHeader(doubao.MsgTypeAudioOnlyReq, doubao.FlagLastData, doubao.SerNone, doubao.CompNone)

		for {
			select {
			case audio, ok := <-audioCh:
				if !ok {
					// Channel closed = end of speech; send the negative/last packet.
					frame := doubao.BuildFrame(lastHeader, nil)
					if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
						log.Printf("doubao asr last packet: %v", err)
					}
					return
				}
				frame := doubao.BuildFrame(audioHeader, audio)
				if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
					log.Printf("doubao asr send audio: %v", err)
					return
				}
			case <-ctx.Done():
				frame := doubao.BuildFrame(lastHeader, nil)
				_ = conn.WriteMessage(websocket.BinaryMessage, frame)
				return
			}
		}
	}()

	// Reader goroutine: receive ASR results and map to ASRResult.
	go func() {
		defer close(resultCh)
		defer conn.Close()

		var prevText string

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				if ctx.Err() == nil && !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					log.Printf("doubao asr read: %v", err)
				}
				return
			}

			h, hErr := doubao.ParseHeader(data)
			if hErr != nil {
				log.Printf("doubao asr parse header: %v", hErr)
				continue
			}

			if h.MsgType == doubao.MsgTypeError {
				code, msg, _ := doubao.ParseErrorResponse(data)
				log.Printf("doubao asr error %d: %s", code, msg)
				return
			}

			if h.MsgType != doubao.MsgTypeFullServerResp {
				continue
			}

			payload, _, isLast, pErr := doubao.ParseServerResponse(data)
			if pErr != nil {
				log.Printf("doubao asr parse response: %v", pErr)
				continue
			}

			var respJSON struct {
				Result struct {
					Text       string `json:"text"`
					Utterances []struct {
						Text     string `json:"text"`
						Definite bool   `json:"definite"`
					} `json:"utterances"`
				} `json:"result"`
			}
			if err := json.Unmarshal(payload, &respJSON); err != nil {
				log.Printf("doubao asr json unmarshal: %v", err)
				continue
			}

			fullText := respJSON.Result.Text
			if fullText == "" {
				if isLast {
					return
				}
				continue
			}

			hasDefinite := false
			for _, u := range respJSON.Result.Utterances {
				if u.Definite {
					hasDefinite = true
					break
				}
			}

			if isLast || hasDefinite {
				// Final result: send the full accumulated text.
				result := ASRResult{
					Text:    fullText,
					IsFinal: true,
					Mode:    "2pass-offline",
				}
				select {
				case resultCh <- result:
				case <-ctx.Done():
					return
				}
				prevText = fullText
				if isLast {
					return
				}
			} else {
				// Partial result: compute delta from previous full text.
				delta := fullText
				if strings.HasPrefix(fullText, prevText) {
					delta = fullText[len(prevText):]
				}
				if delta == "" {
					continue
				}
				prevText = fullText
				result := ASRResult{
					Text:    delta,
					IsFinal: false,
					Mode:    "streaming",
				}
				select {
				case resultCh <- result:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return resultCh, nil
}
