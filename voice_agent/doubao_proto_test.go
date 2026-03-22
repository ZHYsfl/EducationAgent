package main

import (
	"encoding/binary"
	"testing"
)

// ===========================================================================
// buildHeader
// ===========================================================================

func TestBuildHeader(t *testing.T) {
	h := buildHeader(msgTypeFullClientReq, flagNoSeq, serJSON, compGzip)
	if h[0] != 0x11 {
		t.Errorf("byte[0] = 0x%02X, want 0x11", h[0])
	}
	if (h[1]>>4)&0x0F != msgTypeFullClientReq {
		t.Errorf("msg_type = 0x%X", (h[1]>>4)&0x0F)
	}
	if h[1]&0x0F != flagNoSeq {
		t.Errorf("flags = 0x%X", h[1]&0x0F)
	}
	if (h[2]>>4)&0x0F != serJSON {
		t.Errorf("serialization = 0x%X", (h[2]>>4)&0x0F)
	}
	if h[2]&0x0F != compGzip {
		t.Errorf("compression = 0x%X", h[2]&0x0F)
	}
}

func TestBuildHeader_AudioOnly(t *testing.T) {
	h := buildHeader(msgTypeAudioOnlyReq, flagLastData, serNone, compNone)
	if (h[1]>>4)&0x0F != msgTypeAudioOnlyReq {
		t.Errorf("msg_type = 0x%X", (h[1]>>4)&0x0F)
	}
	if h[1]&0x0F != flagLastData {
		t.Errorf("flags = 0x%X", h[1]&0x0F)
	}
}

// ===========================================================================
// buildFrame
// ===========================================================================

func TestBuildFrame(t *testing.T) {
	h := buildHeader(msgTypeFullClientReq, flagNoSeq, serJSON, compNone)
	payload := []byte(`{"key":"value"}`)
	frame := buildFrame(h, payload)

	if len(frame) != 4+4+len(payload) {
		t.Fatalf("frame len = %d, want %d", len(frame), 4+4+len(payload))
	}
	payloadSize := binary.BigEndian.Uint32(frame[4:8])
	if int(payloadSize) != len(payload) {
		t.Errorf("payload_size = %d, want %d", payloadSize, len(payload))
	}
	if string(frame[8:]) != string(payload) {
		t.Errorf("payload mismatch")
	}
}

func TestBuildFrame_EmptyPayload(t *testing.T) {
	h := buildHeader(msgTypeAudioOnlyReq, flagLastData, serNone, compNone)
	frame := buildFrame(h, nil)
	if len(frame) != 8 {
		t.Fatalf("empty payload frame len = %d, want 8", len(frame))
	}
	payloadSize := binary.BigEndian.Uint32(frame[4:8])
	if payloadSize != 0 {
		t.Errorf("payload_size = %d, want 0", payloadSize)
	}
}

// ===========================================================================
// parseHeader
// ===========================================================================

func TestParseHeader(t *testing.T) {
	h := buildHeader(msgTypeFullServerResp, flagPosSeq, serJSON, compGzip)
	ph, err := parseHeader(h[:])
	if err != nil {
		t.Fatal(err)
	}
	if ph.MsgType != msgTypeFullServerResp {
		t.Errorf("MsgType = 0x%X", ph.MsgType)
	}
	if ph.Flags != flagPosSeq {
		t.Errorf("Flags = 0x%X", ph.Flags)
	}
	if ph.Serialization != serJSON {
		t.Errorf("Serialization = 0x%X", ph.Serialization)
	}
	if ph.Compression != compGzip {
		t.Errorf("Compression = 0x%X", ph.Compression)
	}
}

func TestParseHeader_TooShort(t *testing.T) {
	_, err := parseHeader([]byte{0x11, 0x90})
	if err == nil {
		t.Fatal("expected error for short header")
	}
}

// ===========================================================================
// parseServerResponse
// ===========================================================================

func TestParseServerResponse_NoSeq(t *testing.T) {
	h := buildHeader(msgTypeFullServerResp, flagNoSeq, serJSON, compNone)
	payload := []byte(`{"result":"ok"}`)
	frame := buildFrame(h, payload)

	got, seq, isLast, err := parseServerResponse(frame)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Errorf("payload = %q", string(got))
	}
	if seq != 0 {
		t.Errorf("seq = %d", seq)
	}
	if isLast {
		t.Error("should not be last with flagNoSeq")
	}
}

func TestParseServerResponse_WithSeq(t *testing.T) {
	h := buildHeader(msgTypeFullServerResp, flagPosSeq, serJSON, compNone)
	payload := []byte(`{"text":"hello"}`)

	frame := make([]byte, 4+4+4+len(payload))
	copy(frame[0:4], h[:])
	binary.BigEndian.PutUint32(frame[4:8], 42)
	binary.BigEndian.PutUint32(frame[8:12], uint32(len(payload)))
	copy(frame[12:], payload)

	got, seq, _, err := parseServerResponse(frame)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 42 {
		t.Errorf("seq = %d, want 42", seq)
	}
	if string(got) != string(payload) {
		t.Errorf("payload = %q", string(got))
	}
}

func TestParseServerResponse_LastData(t *testing.T) {
	h := buildHeader(msgTypeFullServerResp, flagLastData, serJSON, compNone)
	frame := buildFrame(h, []byte(`{}`))

	_, _, isLast, err := parseServerResponse(frame)
	if err != nil {
		t.Fatal(err)
	}
	if !isLast {
		t.Error("should be last with flagLastData")
	}
}

func TestParseServerResponse_NegSeq(t *testing.T) {
	h := buildHeader(msgTypeFullServerResp, flagNegSeq, serJSON, compNone)
	frame := make([]byte, 4+4+4+2)
	copy(frame[0:4], h[:])
	binary.BigEndian.PutUint32(frame[4:8], uint32(0xFFFFFFFF))
	binary.BigEndian.PutUint32(frame[8:12], 2)
	frame[12] = '{' 
	frame[13] = '}'

	_, seq, isLast, err := parseServerResponse(frame)
	if err != nil {
		t.Fatal(err)
	}
	if !isLast {
		t.Error("should be last with flagNegSeq")
	}
	if seq != -1 {
		t.Errorf("seq = %d, want -1", seq)
	}
}

func TestParseServerResponse_WithGzip(t *testing.T) {
	originalPayload := []byte(`{"compressed":"data"}`)
	compressed, err := gzipCompress(originalPayload)
	if err != nil {
		t.Fatal(err)
	}

	h := buildHeader(msgTypeFullServerResp, flagNoSeq, serJSON, compGzip)
	frame := buildFrame(h, compressed)

	got, _, _, err := parseServerResponse(frame)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(originalPayload) {
		t.Errorf("payload = %q, want %q", string(got), string(originalPayload))
	}
}

func TestParseServerResponse_TooShort(t *testing.T) {
	_, _, _, err := parseServerResponse([]byte{0x11})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestParseServerResponse_MissingPayloadSize(t *testing.T) {
	h := buildHeader(msgTypeFullServerResp, flagNoSeq, serJSON, compNone)
	_, _, _, err := parseServerResponse(h[:])
	if err == nil {
		t.Fatal("expected error for missing payload size")
	}
}

func TestParseServerResponse_PayloadTruncated(t *testing.T) {
	h := buildHeader(msgTypeFullServerResp, flagNoSeq, serJSON, compNone)
	frame := make([]byte, 8)
	copy(frame[0:4], h[:])
	binary.BigEndian.PutUint32(frame[4:8], 1000) // claims 1000 bytes
	_, _, _, err := parseServerResponse(frame)
	if err == nil {
		t.Fatal("expected error for truncated payload")
	}
}

func TestParseServerResponse_MissingSeqData(t *testing.T) {
	h := buildHeader(msgTypeFullServerResp, flagPosSeq, serJSON, compNone)
	_, _, _, err := parseServerResponse(h[:])
	if err == nil {
		t.Fatal("expected error for missing sequence")
	}
}

// ===========================================================================
// parseAudioResponse
// ===========================================================================

func TestParseAudioResponse_NoSeq(t *testing.T) {
	h := buildHeader(msgTypeAudioOnlyResp, flagNoSeq, serNone, compNone)
	audio := []byte{0x01, 0x02, 0x03, 0x04}
	data := append(h[:], audio...)

	got, isLast, err := parseAudioResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Errorf("audio len = %d", len(got))
	}
	if isLast {
		t.Error("should not be last")
	}
}

func TestParseAudioResponse_WithSeq(t *testing.T) {
	h := buildHeader(msgTypeAudioOnlyResp, flagPosSeq, serNone, compNone)
	data := make([]byte, 4+4+5)
	copy(data[0:4], h[:])
	binary.BigEndian.PutUint32(data[4:8], 1)
	copy(data[8:], []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE})

	got, _, err := parseAudioResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("audio len = %d", len(got))
	}
}

func TestParseAudioResponse_Last(t *testing.T) {
	h := buildHeader(msgTypeAudioOnlyResp, flagLastData, serNone, compNone)
	data := append(h[:], []byte{0xFF}...)

	_, isLast, err := parseAudioResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !isLast {
		t.Error("should be last")
	}
}

func TestParseAudioResponse_MissingSeqData(t *testing.T) {
	h := buildHeader(msgTypeAudioOnlyResp, flagPosSeq, serNone, compNone)
	_, _, err := parseAudioResponse(h[:])
	if err == nil {
		t.Fatal("expected error for missing sequence")
	}
}

func TestParseAudioResponse_WithGzip(t *testing.T) {
	originalAudio := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	compressed, err := gzipCompress(originalAudio)
	if err != nil {
		t.Fatal(err)
	}

	h := buildHeader(msgTypeAudioOnlyResp, flagNoSeq, serNone, compGzip)
	data := append(h[:], compressed...)

	got, _, err := parseAudioResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(originalAudio) {
		t.Errorf("audio len = %d, want %d", len(got), len(originalAudio))
	}
}

func TestParseAudioResponse_TooShort(t *testing.T) {
	_, _, err := parseAudioResponse([]byte{0x01})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ===========================================================================
// parseErrorResponse
// ===========================================================================

func TestParseErrorResponse_Success(t *testing.T) {
	data := make([]byte, 12+5)
	hdr := buildHeader(msgTypeError, 0, 0, 0)
	copy(data[0:4], hdr[:])
	binary.BigEndian.PutUint32(data[4:8], 10001)
	binary.BigEndian.PutUint32(data[8:12], 5)
	copy(data[12:], "error")

	code, msg, err := parseErrorResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if code != 10001 {
		t.Errorf("code = %d", code)
	}
	if msg != "error" {
		t.Errorf("msg = %q", msg)
	}
}

func TestParseErrorResponse_TooShort(t *testing.T) {
	_, _, err := parseErrorResponse([]byte{1, 2, 3, 4})
	if err == nil {
		t.Fatal("expected error for short frame")
	}
}

func TestParseErrorResponse_MsgTruncated(t *testing.T) {
	data := make([]byte, 12)
	binary.BigEndian.PutUint32(data[4:8], 1)
	binary.BigEndian.PutUint32(data[8:12], 100) // claims 100 bytes of msg
	_, _, err := parseErrorResponse(data)
	if err == nil {
		t.Fatal("expected error for truncated message")
	}
}

// ===========================================================================
// gzipCompress / gzipDecompress
// ===========================================================================

func TestGzipRoundtrip(t *testing.T) {
	original := []byte("hello world, this is a test of compression")
	compressed, err := gzipCompress(original)
	if err != nil {
		t.Fatal(err)
	}
	if len(compressed) == 0 {
		t.Error("compressed should not be empty")
	}

	decompressed, err := gzipDecompress(compressed)
	if err != nil {
		t.Fatal(err)
	}
	if string(decompressed) != string(original) {
		t.Errorf("roundtrip failed: %q", string(decompressed))
	}
}

func TestGzipDecompress_Invalid(t *testing.T) {
	_, err := gzipDecompress([]byte("not gzip data"))
	if err == nil {
		t.Fatal("expected error for invalid gzip")
	}
}

func TestGzipCompress_Empty(t *testing.T) {
	compressed, err := gzipCompress(nil)
	if err != nil {
		t.Fatal(err)
	}
	decompressed, err := gzipDecompress(compressed)
	if err != nil {
		t.Fatal(err)
	}
	if len(decompressed) != 0 {
		t.Errorf("expected empty, got %d bytes", len(decompressed))
	}
}
