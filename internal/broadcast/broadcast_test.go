package broadcast

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBroadcaster_SubscribePublishReceive(t *testing.T) {
	b := New()
	_, ch := b.Subscribe("global")

	b.Publish("global", []byte("{}"))

	select {
	case msg := <-ch:
		assert.Equal(t, []byte("{}"), msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestBroadcaster_MultipleSubscribers(t *testing.T) {
	b := New()
	_, ch1 := b.Subscribe("global")
	_, ch2 := b.Subscribe("global")

	b.Publish("global", []byte("{}"))

	for _, ch := range []<-chan []byte{ch1, ch2} {
		select {
		case msg := <-ch:
			assert.Equal(t, []byte("{}"), msg)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for message")
		}
	}
}

func TestBroadcaster_TopicIsolation(t *testing.T) {
	b := New()
	_, ch1 := b.Subscribe("job:1")
	_, ch2 := b.Subscribe("job:2")

	b.Publish("job:1", []byte("for-job-1"))

	select {
	case msg := <-ch1:
		assert.Equal(t, []byte("for-job-1"), msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message on job:1")
	}

	select {
	case <-ch2:
		t.Fatal("job:2 subscriber should not receive job:1 event")
	case <-time.After(50 * time.Millisecond):
		// Expected: no message
	}
}

func TestBroadcaster_PublishNoSubscribers(t *testing.T) {
	b := New()
	// Should not panic
	b.Publish("global", []byte("{}"))
}

func TestBroadcaster_SlowClientDropped(t *testing.T) {
	b := New()
	_, slowCh := b.Subscribe("global")
	_, fastCh := b.Subscribe("global")

	// Fill the slow client's buffer (buffer size is 16)
	for i := 0; i < 20; i++ {
		b.Publish("global", []byte("{}"))
	}

	// Fast client should have received messages (up to buffer size)
	received := 0
	for {
		select {
		case <-fastCh:
			received++
		default:
			goto donefast
		}
	}
donefast:
	assert.Equal(t, 16, received, "fast client should receive up to buffer size")

	// Slow client should also have buffer-size messages (first 16)
	received = 0
	for {
		select {
		case <-slowCh:
			received++
		default:
			goto doneslow
		}
	}
doneslow:
	assert.Equal(t, 16, received, "slow client should have buffer-size messages")
}

func TestBroadcaster_Unsubscribe(t *testing.T) {
	b := New()
	id, ch := b.Subscribe("global")

	b.Unsubscribe("global", id)

	// Channel should be closed after unsubscribe
	_, open := <-ch
	assert.False(t, open, "channel should be closed after unsubscribe")

	// Publish after unsubscribe should not panic
	b.Publish("global", []byte("{}"))
}

func TestBroadcaster_UnsubscribeTwice(t *testing.T) {
	b := New()
	id, _ := b.Subscribe("global")

	b.Unsubscribe("global", id)
	// Should not panic
	b.Unsubscribe("global", id)
}

func TestBroadcaster_UnsubscribeRemovesEmptyTopic(t *testing.T) {
	b := New()
	id, _ := b.Subscribe("global")

	b.Unsubscribe("global", id)

	b.mu.RLock()
	_, exists := b.subscribers["global"]
	b.mu.RUnlock()
	assert.False(t, exists, "empty topic should be removed from map")
}

func TestBroadcaster_UnsubscribeNonexistentTopic(t *testing.T) {
	b := New()
	// Should not panic
	b.Unsubscribe("nonexistent", 999)
}

func TestBroadcaster_ConcurrentAccess(t *testing.T) {
	b := New()
	var wg sync.WaitGroup

	// Concurrent subscribes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, ch := b.Subscribe("global")
			// Drain channel to avoid blocking publishers
			go func() {
				for range ch {
				}
			}()
			// Unsubscribe after a bit
			time.Sleep(10 * time.Millisecond)
			b.Unsubscribe("global", id)
		}()
	}

	// Concurrent publishes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Publish("global", []byte("{}"))
		}()
	}

	wg.Wait()
}

func TestBroadcaster_UniqueClientIDs(t *testing.T) {
	b := New()
	id1, _ := b.Subscribe("global")
	id2, _ := b.Subscribe("global")
	id3, _ := b.Subscribe("other")

	assert.NotEqual(t, id1, id2)
	assert.NotEqual(t, id2, id3)
}
