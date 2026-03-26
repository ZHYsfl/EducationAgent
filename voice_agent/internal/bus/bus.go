package bus

import "sync"

type Event struct {
	Type    string
	Payload any
	Source  string
}

type Bus struct {
	subscribers map[string][]chan Event
	mu          sync.RWMutex
}

func New() *Bus {
	return &Bus{
		subscribers: make(map[string][]chan Event),
	}
}

func (b *Bus) Subscribe(eventType string) <-chan Event {
	ch := make(chan Event, 100)
	b.mu.Lock()
	b.subscribers[eventType] = append(b.subscribers[eventType], ch)
	b.mu.Unlock()
	return ch
}

func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	subs := b.subscribers[event.Type]
	b.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}
