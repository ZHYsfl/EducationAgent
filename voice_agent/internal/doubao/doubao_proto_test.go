package doubao

import (
	"encoding/binary"
	"testing"
)

// ===========================================================================
// BuildHeader
// ===========================================================================

func TestBuildHeader(t *testing.T) {
	h := BuildHeader(MsgTypeFullClientReq, FlagNoSeq, SerJSON, CompGzip)
	if h[0] != 0x11 {
		t.Errorf("byte[0] = 0x%02X, want 0x11", h[0])
	}
	if (h[1]>>4)&0x0F != MsgTypeFullClientReq {
		t.Errorf("msg_type = 0x%X", (h[1]>>4)&0x0F)
	}
	if h[1]&0x0F != FlagNoSeq {
		t.Errorf("flags = 0x%X", h[1]&0x0F)
	}
	if (h[2]>>4)&0x0F != SerJSON {
		t.Errorf("serialization = 0x%X", (h[2]>>4)&0x0F)
	}
	if h[2]&0x0F != CompGzip {
		t.Errorf("compression = 0x%X", h[2]&0x0F)
	}
}

func TestBuildHeader_AudioOnly(t *testing.T) {
	h := BuildHeader(MsgTypeAudioOnlyReq, FlagLastData, SerNone, CompNone)
	if (h[1]>>4)&0x0F != MsgTypeAudioOnlyReq {
		t.Errorf("msg_type = 0x%X", (h[1]>>4)&0x0F)
	}
	if h[1]&0x0F != FlagLastData {
		t.Errorf("flags = 0x%X", h[1]&0x0F)
	}
}

// ===========================================================================
// BuildFrame
// ===========================================================================

func TestBuildFrame(t *testing.T) {
	h := BuildHeader(MsgTypeFullClientReq, FlagNoSeq, SerJSON, CompNone)
	payload := []byte(`{"key":"value"}`)
	frame := BuildFrame(h, payload)

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
	h := BuildHeader(MsgTypeAudioOnlyReq, FlagLastData, SerNone, CompNone)
	frame := BuildFrame(h, nil)
	if len(frame) != 8 {
		t.Fatalf("empty payload frame len = %d, want 8", len(frame))
	}
	payloadSize := binary.BigEndian.Uint32(frame[4:8])
	if payloadSize != 0 {
		t.Errorf("payload_size = %d, want 0", payloadSize)
	}
}

// ===========================================================================
// ParseHeader
// ===========================================================================

func TestParseHeader(t *testing.T) {
	h := BuildHeader(MsgTypeFullServerResp, FlagPosSeq, SerJSON, CompGzip)
	ph, err := ParseHeader(h[:])
	if err != nil {
		t.Fatal(err)
	}
	if ph.MsgType != MsgTypeFullServerResp {
		t.Errorf("MsgType = 0x%X", ph.MsgType)
	}
	if ph.Flags != FlagPosSeq {
		t.Errorf("Flags = 0x%X", ph.Flags)
	}
	if ph.Serialization != SerJSON {
		t.Errorf("Serialization = 0x%X", ph.Serialization)
	}
	if ph.Compression != CompGzip {
		t.Errorf("Compression = 0x%X", ph.Compression)
	}
}

func TestParseHeader_TooShort(t *testing.T) {
	_, err := ParseHeader([]byte{0x11, 0x90})
	if err == nil {
		t.Fatal("expected error for short header")
	}
}

// ===========================================================================
// ParseServerResponse
// ===========================================================================

func TestParseServerResponse_NoSeq(t *testing.T) {
	h := BuildHeader(MsgTypeFullServerResp, FlagNoSeq, SerJSON, CompNone)
	payload := []byte(`{"result":"ok"}`)
	frame := BuildFrame(h, payload)

	got, seq, isLast, err := ParseServerResponse(frame)
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
		t.Error("should not be last with FlagNoSeq")
	}
}

func TestParseServerResponse_WithSeq(t *testing.T) {
	h := BuildHeader(MsgTypeFullServerResp, FlagPosSeq, SerJSON, CompNone)
	payload := []byte(`{"text":"hello"}`)

	frame := make([]byte, 4+4+4+len(payload))
	copy(frame[0:4], h[:])
	binary.BigEndian.PutUint32(frame[4:8], 42)
	binary.BigEndian.PutUint32(frame[8:12], uint32(len(payload)))
	copy(frame[12:], payload)

	got, seq, _, err := ParseServerResponse(frame)
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
	h := BuildHeader(MsgTypeFullServerResp, FlagLastData, SerJSON, CompNone)
	frame := BuildFrame(h, []byte(`{}`))

	_, _, isLast, err := ParseServerResponse(frame)
	if err != nil {
		t.Fatal(err)
	}
	if !isLast {
		t.Error("should be last with FlagLastData")
	}
}

func TestParseServerResponse_NegSeq(t *testing.T) {
	h := BuildHeader(MsgTypeFullServerResp, FlagNegSeq, SerJSON, CompNone)
	frame := make([]byte, 4+4+4+2)
	copy(frame[0:4], h[:])
	binary.BigEndian.PutUint32(frame[4:8], uint32(0xFFFFFFFF))
	binary.BigEndian.PutUint32(frame[8:12], 2)
	frame[12] = '{'
	frame[13] = '}'

	_, seq, isLast, err := ParseServerResponse(frame)
	if err != nil {
		t.Fatal(err)
	}
	if !isLast {
		t.Error("should be last with FlagNegSeq")
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

	h := BuildHeader(MsgTypeFullServerResp, FlagNoSeq, SerJSON, CompGzip)
	frame := BuildFrame(h, compressed)

	got, _, _, err := ParseServerResponse(frame)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(originalPayload) {
		t.Errorf("payload = %q, want %q", string(got), string(originalPayload))
	}
}

func TestParseServerResponse_TooShort(t *testing.T) {
	_, _, _, err := ParseServerResponse([]byte{0x11})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestParseServerResponse_MissingPayloadSize(t *testing.T) {
	h := BuildHeader(MsgTypeFullServerResp, FlagNoSeq, SerJSON, CompNone)
	_, _, _, err := ParseServerResponse(h[:])
	if err == nil {
		t.Fatal("expected error for missing payload size")
	}
}

func TestParseServerResponse_PayloadTruncated(t *testing.T) {
	h := BuildHeader(MsgTypeFullServerResp, FlagNoSeq, SerJSON, CompNone)
	frame := make([]byte, 8)
	copy(frame[0:4], h[:])
	binary.BigEndian.PutUint32(frame[4:8], 1000)
	_, _, _, err := ParseServerResponse(frame)
	if err == nil {
		t.Fatal("expected error for truncated payload")
	}
}

func TestParseServerResponse_MissingSeqData(t *testing.T) {
	h := BuildHeader(MsgTypeFullServerResp, FlagPosSeq, SerJSON, CompNone)
	_, _, _, err := ParseServerResponse(h[:])
	if err == nil {
		t.Fatal("expected error for missing sequence")
	}
}

// ===========================================================================
// ParseAudioResponse
// ===========================================================================

func TestParseAudioResponse_NoSeq(t *testing.T) {
	h := BuildHeader(MsgTypeAudioOnlyResp, FlagNoSeq, SerNone, CompNone)
	audio := []byte{0x01, 0x02, 0x03, 0x04}
	data := append(h[:], audio...)

	got, isLast, err := ParseAudioResponse(data)
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
	h := BuildHeader(MsgTypeAudioOnlyResp, FlagPosSeq, SerNone, CompNone)
	data := make([]byte, 4+4+5)
	copy(data[0:4], h[:])
	binary.BigEndian.PutUint32(data[4:8], 1)
	copy(data[8:], []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE})

	got, _, err := ParseAudioResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Errorf("audio len = %d", len(got))
	}
}

func TestParseAudioResponse_Last(t *testing.T) {
	h := BuildHeader(MsgTypeAudioOnlyResp, FlagLastData, SerNone, CompNone)
	data := append(h[:], []byte{0xFF}...)

	_, isLast, err := ParseAudioResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !isLast {
		t.Error("should be last")
	}
}

func TestParseAudioResponse_MissingSeqData(t *testing.T) {
	h := BuildHeader(MsgTypeAudioOnlyResp, FlagPosSeq, SerNone, CompNone)
	_, _, err := ParseAudioResponse(h[:])
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

	h := BuildHeader(MsgTypeAudioOnlyResp, FlagNoSeq, SerNone, CompGzip)
	data := append(h[:], compressed...)

	got, _, err := ParseAudioResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(originalAudio) {
		t.Errorf("audio len = %d, want %d", len(got), len(originalAudio))
	}
}

func TestParseAudioResponse_TooShort(t *testing.T) {
	_, _, err := ParseAudioResponse([]byte{0x01})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ===========================================================================
// ParseErrorResponse
// ===========================================================================

func TestParseErrorResponse_Success(t *testing.T) {
	data := make([]byte, 12+5)
	hdr := BuildHeader(MsgTypeError, 0, 0, 0)
	copy(data[0:4], hdr[:])
	binary.BigEndian.PutUint32(data[4:8], 10001)
	binary.BigEndian.PutUint32(data[8:12], 5)
	copy(data[12:], "error")

	code, msg, err := ParseErrorResponse(data)
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
	_, _, err := ParseErrorResponse([]byte{1, 2, 3, 4})
	if err == nil {
		t.Fatal("expected error for short frame")
	}
}

func TestParseErrorResponse_MsgTruncated(t *testing.T) {
	data := make([]byte, 12)
	binary.BigEndian.PutUint32(data[4:8], 1)
	binary.BigEndian.PutUint32(data[8:12], 100)
	_, _, err := ParseErrorResponse(data)
	if err == nil {
		t.Fatal("expected error for truncated message")
	}
}
