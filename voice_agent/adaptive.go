package main

import (
	"encoding/json"
	"log"
	"math"
	"os"
	"sync"
	"time"
)

type ChannelSizes struct {
	AudioCh     int       `json:"audio_ch"`
	ASRAudioCh  int       `json:"asr_audio_ch"`
	ASRResultCh int       `json:"asr_result_ch"`
	SentenceCh  int       `json:"sentence_ch"`
	WriteCh     int       `json:"write_ch"`
	TTSChunkCh  int       `json:"tts_chunk_ch"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type channelSpec struct {
	Min int
	Max int
}

var channelSpecs = map[string]channelSpec{
	"audio_ch":      {Min: 50, Max: 800},
	"asr_audio_ch":  {Min: 5, Max: 80},
	"asr_result_ch": {Min: 5, Max: 80},
	"sentence_ch":   {Min: 5, Max: 80},
	"write_ch":      {Min: 64, Max: 1024},
	"tts_chunk_ch":  {Min: 5, Max: 80},
}

type ChannelMetrics struct {
	PeakLen    int
	SendBlocks int
}

type AdaptiveController struct {
	mu      sync.RWMutex
	sizes   ChannelSizes
	metrics map[string]*ChannelMetrics
}

func DefaultChannelSizes() ChannelSizes {
	return ChannelSizes{
		AudioCh:     200,
		ASRAudioCh:  20,
		ASRResultCh: 20,
		SentenceCh:  20,
		WriteCh:     256,
		TTSChunkCh:  20,
	}
}

func NewAdaptiveController(initial ChannelSizes) *AdaptiveController {
	return &AdaptiveController{
		sizes:   initial,
		metrics: make(map[string]*ChannelMetrics),
	}
}

func (ac *AdaptiveController) Get(name string) int {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	switch name {
	case "audio_ch":
		return ac.sizes.AudioCh
	case "asr_audio_ch":
		return ac.sizes.ASRAudioCh
	case "asr_result_ch":
		return ac.sizes.ASRResultCh
	case "sentence_ch":
		return ac.sizes.SentenceCh
	case "write_ch":
		return ac.sizes.WriteCh
	case "tts_chunk_ch":
		return ac.sizes.TTSChunkCh
	default:
		return 20
	}
}

func (ac *AdaptiveController) RecordLen(name string, length int) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	m, ok := ac.metrics[name]
	if !ok {
		m = &ChannelMetrics{}
		ac.metrics[name] = m
	}
	if length > m.PeakLen {
		m.PeakLen = length
	}
}

func (ac *AdaptiveController) RecordBlock(name string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	m, ok := ac.metrics[name]
	if !ok {
		m = &ChannelMetrics{}
		ac.metrics[name] = m
	}
	m.SendBlocks++
}

func (ac *AdaptiveController) Adjust() {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	adjust := func(name string, current int) int {
		m, ok := ac.metrics[name]
		if !ok {
			return current
		}
		spec := channelSpecs[name]
		utilization := float64(m.PeakLen) / float64(current)

		newSize := current
		if utilization > 0.8 || m.SendBlocks > 0 {
			newSize = int(math.Ceil(float64(current) * 1.5))
		} else if utilization < 0.2 && m.SendBlocks == 0 {
			newSize = int(math.Floor(float64(current) * 0.75))
		}
		if newSize < spec.Min {
			newSize = spec.Min
		}
		if newSize > spec.Max {
			newSize = spec.Max
		}
		return newSize
	}

	oldSizes := ac.sizes
	ac.sizes.AudioCh = adjust("audio_ch", ac.sizes.AudioCh)
	ac.sizes.ASRAudioCh = adjust("asr_audio_ch", ac.sizes.ASRAudioCh)
	ac.sizes.ASRResultCh = adjust("asr_result_ch", ac.sizes.ASRResultCh)
	ac.sizes.SentenceCh = adjust("sentence_ch", ac.sizes.SentenceCh)
	ac.sizes.WriteCh = adjust("write_ch", ac.sizes.WriteCh)
	ac.sizes.TTSChunkCh = adjust("tts_chunk_ch", ac.sizes.TTSChunkCh)
	ac.sizes.UpdatedAt = time.Now()

	if ac.sizes != oldSizes {
		log.Printf("Adaptive channel sizes adjusted: audio=%d asr_audio=%d asr_result=%d sentence=%d write=%d tts=%d",
			ac.sizes.AudioCh, ac.sizes.ASRAudioCh, ac.sizes.ASRResultCh,
			ac.sizes.SentenceCh, ac.sizes.WriteCh, ac.sizes.TTSChunkCh)
	}

	ac.metrics = make(map[string]*ChannelMetrics)
}

func (ac *AdaptiveController) Save(path string) {
	ac.mu.RLock()
	data, err := json.MarshalIndent(ac.sizes, "", "  ")
	ac.mu.RUnlock()
	if err != nil {
		log.Printf("adaptive save marshal: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("adaptive save write: %v", err)
	}
}

func LoadChannelSizes(path string, fallback ChannelSizes) ChannelSizes {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var sizes ChannelSizes
	if err := json.Unmarshal(data, &sizes); err != nil {
		log.Printf("adaptive load parse error, using defaults: %v", err)
		return fallback
	}
	sizes = clampSizes(sizes)
	log.Printf("Loaded adaptive sizes from %s: audio=%d asr_audio=%d asr_result=%d sentence=%d write=%d tts=%d",
		path, sizes.AudioCh, sizes.ASRAudioCh, sizes.ASRResultCh,
		sizes.SentenceCh, sizes.WriteCh, sizes.TTSChunkCh)
	return sizes
}

func clampSizes(s ChannelSizes) ChannelSizes {
	clamp := func(val int, name string) int {
		spec := channelSpecs[name]
		if val < spec.Min {
			return spec.Min
		}
		if val > spec.Max {
			return spec.Max
		}
		if val == 0 {
			return DefaultChannelSizes().AudioCh // will be overridden below
		}
		return val
	}
	s.AudioCh = clamp(s.AudioCh, "audio_ch")
	s.ASRAudioCh = clamp(s.ASRAudioCh, "asr_audio_ch")
	s.ASRResultCh = clamp(s.ASRResultCh, "asr_result_ch")
	s.SentenceCh = clamp(s.SentenceCh, "sentence_ch")
	s.WriteCh = clamp(s.WriteCh, "write_ch")
	s.TTSChunkCh = clamp(s.TTSChunkCh, "tts_chunk_ch")
	return s
}
