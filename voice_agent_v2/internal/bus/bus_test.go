package bus

import (
	"sync"
	"testing"
	"time"
)

func TestBusConcurrent(t *testing.T) {
	b := New()

	var wg sync.WaitGroup
	subscribers := 10
	messages := 100

	for i := 0; i < subscribers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := b.Subscribe("test")
			count := 0
			timeout := time.After(2 * time.Second)
			for {
				select {
				case <-ch:
					count++
					if count >= messages {
						return
					}
				case <-timeout:
					t.Errorf("timeout waiting for messages, got %d", count)
					return
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)

	for i := 0; i < messages; i++ {
		b.Publish(Event{Type: "test", Payload: i})
	}

	wg.Wait()
}
