package adaptive_test

import (
	"sync"
	"testing"

	adaptivepkg "voiceagent/internal/adaptive"
)

func TestAdaptiveController_ConcurrentGet(t *testing.T) {
	ac := adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes())
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_ = ac.Get("audio_ch")
				_ = ac.Get("asr_audio_ch")
				_ = ac.Get("write_ch")
			}
		}()
	}
	wg.Wait()
}

func TestAdaptiveController_ConcurrentRecordLen(t *testing.T) {
	ac := adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				ac.RecordLen("audio_ch", id*10+j)
				ac.RecordLen("write_ch", id*5+j)
			}
		}(i)
	}
	wg.Wait()
}

func TestAdaptiveController_ConcurrentRecordBlock(t *testing.T) {
	ac := adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				ac.RecordBlock("audio_ch")
				ac.RecordBlock("sentence_ch")
			}
		}()
	}
	wg.Wait()
}

func TestAdaptiveController_ConcurrentMixed(t *testing.T) {
	ac := adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes())
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = ac.Get("audio_ch")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				ac.RecordLen("audio_ch", j)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				ac.Adjust()
			}
		}()
	}
	wg.Wait()
}
