package adaptive_test

import (
	"testing"

	adaptivepkg "voiceagent/internal/adaptive"
)

func TestAdaptiveController_DefaultGet(t *testing.T) {
	ac := adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes())
	if v := ac.Get("audio_ch"); v != 200 {
		t.Errorf("audio_ch = %d, want 200", v)
	}
	if v := ac.Get("unknown"); v != 20 {
		t.Errorf("unknown = %d, want 20", v)
	}
}

func TestAdaptiveController_RecordAndAdjust_HighUtil(t *testing.T) {
	ac := adaptivepkg.NewAdaptiveController(adaptivepkg.ChannelSizes{
		AudioCh:     100,
		ASRAudioCh:  20,
		ASRResultCh: 20,
		SentenceCh:  20,
		WriteCh:     100,
		TTSChunkCh:  20,
	})
	for i := 0; i < 5; i++ {
		ac.RecordLen("audio_ch", 90)
	}
	ac.Adjust()
	if v := ac.Get("audio_ch"); v <= 100 {
		t.Errorf("expected audio_ch > 100 after high utilization, got %d", v)
	}
}

func TestAdaptiveController_RecordAndAdjust_LowUtil(t *testing.T) {
	ac := adaptivepkg.NewAdaptiveController(adaptivepkg.ChannelSizes{
		AudioCh:     400,
		ASRAudioCh:  20,
		ASRResultCh: 20,
		SentenceCh:  20,
		WriteCh:     100,
		TTSChunkCh:  20,
	})
	ac.RecordLen("audio_ch", 10)
	ac.Adjust()
	if v := ac.Get("audio_ch"); v >= 400 {
		t.Errorf("expected audio_ch < 400 after low utilization, got %d", v)
	}
}

func TestAdaptiveController_ClampMin(t *testing.T) {
	ac := adaptivepkg.NewAdaptiveController(adaptivepkg.ChannelSizes{
		AudioCh:     50,
		ASRAudioCh:  5,
		ASRResultCh: 5,
		SentenceCh:  5,
		WriteCh:     64,
		TTSChunkCh:  5,
	})
	ac.RecordLen("audio_ch", 1)
	ac.Adjust()
	if v := ac.Get("audio_ch"); v < 50 {
		t.Errorf("expected audio_ch >= 50 (min), got %d", v)
	}
}

func TestAdaptiveController_RecordBlock(t *testing.T) {
	ac := adaptivepkg.NewAdaptiveController(adaptivepkg.ChannelSizes{
		AudioCh:     100,
		ASRAudioCh:  20,
		ASRResultCh: 20,
		SentenceCh:  20,
		WriteCh:     100,
		TTSChunkCh:  20,
	})
	ac.RecordBlock("audio_ch")
	ac.RecordBlock("audio_ch")

	ac.Adjust()
	if v := ac.Get("audio_ch"); v <= 100 {
		t.Errorf("expected audio_ch > 100 after blocks, got %d", v)
	}
}

func TestAdaptiveController_Save(t *testing.T) {
	ac := adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes())
	tmpFile := t.TempDir() + "/adaptive_test.json"
	ac.Save(tmpFile)

	loaded := adaptivepkg.LoadChannelSizes(tmpFile, adaptivepkg.ChannelSizes{})
	if loaded.AudioCh != 200 {
		t.Errorf("loaded AudioCh = %d, want 200", loaded.AudioCh)
	}
}

func TestAdaptiveController_GetAllChannels(t *testing.T) {
	ac := adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes())
	names := []string{"audio_ch", "asr_audio_ch", "asr_result_ch", "sentence_ch", "write_ch", "tts_chunk_ch"}
	expected := []int{200, 20, 20, 20, 256, 20}
	for i, name := range names {
		if v := ac.Get(name); v != expected[i] {
			t.Errorf("%s = %d, want %d", name, v, expected[i])
		}
	}
}

func TestLoadChannelSizes_Fallback(t *testing.T) {
	sizes := adaptivepkg.LoadChannelSizes("/nonexistent/path.json", adaptivepkg.DefaultChannelSizes())
	if sizes.AudioCh != 200 {
		t.Errorf("expected fallback AudioCh=200, got %d", sizes.AudioCh)
	}
}

func TestClampChannelSizes(t *testing.T) {
	s := adaptivepkg.ChannelSizes{
		AudioCh:     1,
		ASRAudioCh:  1000,
		ASRResultCh: 20,
		SentenceCh:  20,
		WriteCh:     100,
		TTSChunkCh:  20,
	}
	clamped := adaptivepkg.ClampChannelSizes(s)
	if clamped.AudioCh < 50 {
		t.Errorf("AudioCh should be clamped to min 50, got %d", clamped.AudioCh)
	}
	if clamped.ASRAudioCh > 80 {
		t.Errorf("ASRAudioCh should be clamped to max 80, got %d", clamped.ASRAudioCh)
	}
}

func TestClampChannelSizes_ZeroValues(t *testing.T) {
	s := adaptivepkg.ChannelSizes{
		AudioCh:     0,
		ASRAudioCh:  0,
		ASRResultCh: 0,
		SentenceCh:  0,
		WriteCh:     0,
		TTSChunkCh:  0,
	}
	clamped := adaptivepkg.ClampChannelSizes(s)
	defaults := adaptivepkg.DefaultChannelSizes()
	if clamped.AudioCh != defaults.AudioCh {
		t.Errorf("AudioCh = %d, want %d", clamped.AudioCh, defaults.AudioCh)
	}
	if clamped.ASRAudioCh != defaults.ASRAudioCh {
		t.Errorf("ASRAudioCh = %d, want %d", clamped.ASRAudioCh, defaults.ASRAudioCh)
	}
	if clamped.WriteCh != defaults.WriteCh {
		t.Errorf("WriteCh = %d, want %d", clamped.WriteCh, defaults.WriteCh)
	}
}
