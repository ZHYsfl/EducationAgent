package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var doubaoTTSEndpoint = "wss://openspeech.bytedance.com/api/v1/tts/ws_binary"

type DouBaoTTSConfig struct {
	AppId     string
	Token     string
	Cluster   string
	VoiceType string
}

type DouBaoTTSClient struct {
	config DouBaoTTSConfig
}

func NewDouBaoTTSClient(cfg DouBaoTTSConfig) *DouBaoTTSClient {
	return &DouBaoTTSClient{config: cfg}
}

func (c *DouBaoTTSClient) Synthesize(ctx context.Context, text string, bufSize int) (<-chan []byte, error) {
	ch := make(chan []byte, bufSize)

	header := http.Header{}
	header.Set("Authorization", "Bearer;"+c.config.Token)

	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, doubaoTTSEndpoint, header)
	if err != nil {
		close(ch)
		return ch, fmt.Errorf("doubao tts connect: %w", err)
	}

	reqPayload, err := json.Marshal(map[string]any{
		"app": map[string]any{
			"appid":   c.config.AppId,
			"token":   c.config.Token,
			"cluster": c.config.Cluster,
		},
		"user": map[string]any{
			"uid": "voice_agent",
		},
		"audio": map[string]any{
			"voice_type": c.config.VoiceType,
			"encoding":   "mp3",
			"rate":       24000,
			"speed_ratio": 1.0,
			"volume_ratio": 1.0,
		},
		"request": map[string]any{
			"reqid":     uuid.New().String(),
			"text":      text,
			"operation": "submit",
		},
	})
	if err != nil {
		conn.Close()
		close(ch)
		return ch, fmt.Errorf("doubao tts request marshal: %w", err)
	}

	h := buildHeader(msgTypeFullClientReq, flagNoSeq, serJSON, compNone)
	frame := buildFrame(h, reqPayload)
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		conn.Close()
		close(ch)
		return ch, fmt.Errorf("doubao tts send request: %w", err)
	}

	go func() {
		defer close(ch)
		defer conn.Close()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				if ctx.Err() == nil && !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					log.Printf("doubao tts read: %v", err)
				}
				return
			}

			ph, hErr := parseHeader(data)
			if hErr != nil {
				log.Printf("doubao tts parse header: %v", hErr)
				continue
			}

			switch ph.MsgType {
			case msgTypeError:
				code, msg, _ := parseErrorResponse(data)
				log.Printf("doubao tts error %d: %s", code, msg)
				return

			case msgTypeAudioOnlyResp:
				audio, isLast, aErr := parseAudioResponse(data)
				if aErr != nil {
					log.Printf("doubao tts parse audio: %v", aErr)
					return
				}
				if len(audio) > 0 {
					if ctx.Err() != nil {
						return
					}
					select {
					case ch <- audio:
					case <-ctx.Done():
						return
					}
				}
				if isLast {
					return
				}

			case msgTypeFullServerResp:
				// Some TTS responses may include a server ack with JSON payload;
				// we only care about audio chunks, so skip these.
				continue

			default:
				log.Printf("doubao tts unexpected msg type: 0x%X", ph.MsgType)
			}
		}
	}()

	return ch, nil
}
