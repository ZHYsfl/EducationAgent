package doubao

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
)

// Doubao (Volcano Engine) WebSocket binary protocol constants.
// Both ASR and TTS share the same 4-byte header format.

const (
	protoVersion byte = 0x1

	// Message types (upper 4 bits of byte 1)
	MsgTypeFullClientReq  byte = 0x1
	MsgTypeAudioOnlyReq   byte = 0x2
	MsgTypeFullServerResp byte = 0x9
	MsgTypeAudioOnlyResp  byte = 0xB // TTS audio response
	MsgTypeError          byte = 0xF

	// Message type specific flags (lower 4 bits of byte 1)
	FlagNoSeq    byte = 0x0
	FlagPosSeq   byte = 0x1
	FlagLastData byte = 0x2
	FlagNegSeq   byte = 0x3

	// Serialization (upper 4 bits of byte 2)
	SerNone byte = 0x0
	SerJSON byte = 0x1

	// Compression (lower 4 bits of byte 2)
	CompNone byte = 0x0
	CompGzip byte = 0x1
)

// ParsedHeader holds the decoded fields from a Doubao 4-byte binary header.
type ParsedHeader struct {
	MsgType       byte
	Flags         byte
	Serialization byte
	Compression   byte
}

func BuildHeader(msgType, flags, serialization, compression byte) [4]byte {
	var h [4]byte
	h[0] = (protoVersion << 4) | 0x1 // version=1, header_size=1 (1*4=4 bytes)
	h[1] = (msgType << 4) | flags
	h[2] = (serialization << 4) | compression
	h[3] = 0x00
	return h
}

// BuildFrame assembles header + payload_size(4B big-endian) + payload.
func BuildFrame(header [4]byte, payload []byte) []byte {
	frame := make([]byte, 4+4+len(payload))
	copy(frame[0:4], header[:])
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	copy(frame[8:], payload)
	return frame
}

func ParseHeader(data []byte) (ParsedHeader, error) {
	if len(data) < 4 {
		return ParsedHeader{}, fmt.Errorf("doubao proto: header too short (%d bytes)", len(data))
	}
	return ParsedHeader{
		MsgType:       (data[1] >> 4) & 0x0F,
		Flags:         data[1] & 0x0F,
		Serialization: (data[2] >> 4) & 0x0F,
		Compression:   data[2] & 0x0F,
	}, nil
}

// ParseServerResponse extracts the JSON payload from a full server response.
// Layout: [4B header] [4B sequence (if flags has seq)] [4B payload_size] [payload]
func ParseServerResponse(data []byte) (payload []byte, sequence int32, isLast bool, err error) {
	h, err := ParseHeader(data)
	if err != nil {
		return nil, 0, false, err
	}

	isLast = h.Flags == FlagLastData || h.Flags == FlagNegSeq
	offset := 4

	hasSeq := h.Flags == FlagPosSeq || h.Flags == FlagNegSeq
	if hasSeq {
		if len(data) < offset+4 {
			return nil, 0, false, fmt.Errorf("doubao proto: missing sequence")
		}
		sequence = int32(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4
	}

	if len(data) < offset+4 {
		return nil, sequence, isLast, fmt.Errorf("doubao proto: missing payload size")
	}
	payloadSize := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	if uint32(len(data)-offset) < payloadSize {
		return nil, sequence, isLast, fmt.Errorf("doubao proto: payload truncated")
	}
	payload = data[offset : offset+int(payloadSize)]

	if h.Compression == CompGzip && len(payload) > 0 {
		payload, err = gzipDecompress(payload)
		if err != nil {
			return nil, sequence, isLast, fmt.Errorf("doubao proto: gzip decompress: %w", err)
		}
	}

	return payload, sequence, isLast, nil
}

// ParseAudioResponse extracts audio bytes from a TTS audio-only server response.
// Layout: [4B header] [optional 4B sequence] [audio data to end]
func ParseAudioResponse(data []byte) (audio []byte, isLast bool, err error) {
	h, err := ParseHeader(data)
	if err != nil {
		return nil, false, err
	}

	isLast = h.Flags == FlagLastData || h.Flags == FlagNegSeq
	offset := 4

	hasSeq := h.Flags == FlagPosSeq || h.Flags == FlagNegSeq
	if hasSeq {
		if len(data) < offset+4 {
			return nil, false, fmt.Errorf("doubao proto: missing sequence in audio response")
		}
		offset += 4
	}

	audio = data[offset:]

	if h.Compression == CompGzip && len(audio) > 0 {
		audio, err = gzipDecompress(audio)
		if err != nil {
			return nil, isLast, fmt.Errorf("doubao proto: gzip decompress audio: %w", err)
		}
	}

	return audio, isLast, nil
}

// ParseErrorResponse extracts error code and message from an error frame.
// Layout: [4B header] [4B error_code] [4B msg_size] [msg UTF-8]
func ParseErrorResponse(data []byte) (code uint32, msg string, err error) {
	if len(data) < 12 {
		return 0, "", fmt.Errorf("doubao proto: error frame too short")
	}
	code = binary.BigEndian.Uint32(data[4:8])
	msgSize := binary.BigEndian.Uint32(data[8:12])
	if uint32(len(data)-12) < msgSize {
		return code, "", fmt.Errorf("doubao proto: error message truncated")
	}
	return code, string(data[12 : 12+msgSize]), nil
}

func gzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gzipDecompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// GzipCompress and GzipDecompress expose gzip helpers for black-box tests.
func GzipCompress(data []byte) ([]byte, error) { return gzipCompress(data) }
func GzipDecompress(data []byte) ([]byte, error) { return gzipDecompress(data) }
