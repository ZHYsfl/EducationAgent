package agent

import (
	"testing"
)

// ===========================================================================
// decodeAPIData
// ===========================================================================

func TestDecodeAPIData_WrappedSuccess(t *testing.T) {
	raw := []byte(`{"code":200,"message":"ok","data":{"task_id":"t1"}}`)
	var out PPTInitResponse
	err := decodeAPIData(raw, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.TaskID != "t1" {
		t.Errorf("task_id = %q, want t1", out.TaskID)
	}
}

func TestDecodeAPIData_WrappedError(t *testing.T) {
	raw := []byte(`{"code":50200,"message":"服务不可用"}`)
	var out PPTInitResponse
	err := decodeAPIData(raw, &out)
	if err == nil {
		t.Fatal("expected error for non-200 code")
	}
}

func TestDecodeAPIData_NilOut(t *testing.T) {
	err := decodeAPIData([]byte(`{"code":200}`), nil)
	if err != nil {
		t.Fatalf("nil out should not error, got %v", err)
	}
}

func TestDecodeAPIData_RawJSON(t *testing.T) {
	raw := []byte(`{"task_id":"direct"}`)
	var out PPTInitResponse
	err := decodeAPIData(raw, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.TaskID != "direct" {
		t.Errorf("task_id = %q, want direct", out.TaskID)
	}
}

func TestDecodeAPIData_NullData(t *testing.T) {
	raw := []byte(`{"code":200,"data":null}`)
	var out PPTInitResponse
	err := decodeAPIData(raw, &out)
	if err != nil {
		t.Fatalf("null data should not error, got %v", err)
	}
}
