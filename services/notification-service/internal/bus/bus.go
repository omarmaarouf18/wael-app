// Package bus fans notifications out to live SSE subscribers: Redis
// pub/sub when available, otherwise in-process broadcast (tests, localhost
// without redis). Channel per user: "notif:user:<userID>".
package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/omarmaarouf18/wael-app/notification-service/internal/models"
	"github.com/redis/go-redis/v9"
)

// AccountEvent represents an account or session lifecycle event (e.g. suspension or session revocation).
type AccountEvent struct {
	Action string `json:"action"`
	UserID string `json:"user_id,omitempty"`
	SID    string `json:"sid,omitempty"`
}

// Bus publishes notifications and subscribes per user, and fans out account revocation events.
type Bus interface {
	Publish(ctx context.Context, n *models.Notification) error
	Subscribe(ctx context.Context, userID string) (<-chan *models.Notification, func(), error)
	PublishAccountEvent(ctx context.Context, evt *AccountEvent) error
	SubscribeAccountEvents(ctx context.Context) (<-chan *AccountEvent, func(), error)
}

func channelFor(userID string) string {
	return "notif:user:" + userID
}

const accountEventsChannel = "account:events"

// MemoryBus is an in-process fan-out bus.
type MemoryBus struct {
	mu          sync.RWMutex
	subs        map[string]map[chan *models.Notification]struct{}
	accountSubs map[chan *AccountEvent]struct{}
}

// NewMemoryBus creates an empty MemoryBus.
func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		subs:        map[string]map[chan *models.Notification]struct{}{},
		accountSubs: map[chan *AccountEvent]struct{}{},
	}
}

// Publish delivers to current subscribers (non-blocking).
func (b *MemoryBus) Publish(_ context.Context, n *models.Notification) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs[n.UserID] {
		select {
		case ch <- n:
		default:
		}
	}
	return nil
}

// Subscribe returns a channel fed with the user's notifications.
func (b *MemoryBus) Subscribe(_ context.Context, userID string) (<-chan *models.Notification, func(), error) {
	ch := make(chan *models.Notification, 16)
	b.mu.Lock()
	if b.subs[userID] == nil {
		b.subs[userID] = map[chan *models.Notification]struct{}{}
	}
	b.subs[userID][ch] = struct{}{}
	b.mu.Unlock()
	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if set, ok := b.subs[userID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(b.subs, userID)
			}
		}
		close(ch)
	}
	return ch, unsub, nil
}

// PublishAccountEvent delivers an account event to subscribers (non-blocking).
func (b *MemoryBus) PublishAccountEvent(_ context.Context, evt *AccountEvent) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.accountSubs {
		select {
		case ch <- evt:
		default:
		}
	}
	return nil
}

// SubscribeAccountEvents returns a channel fed with account events.
func (b *MemoryBus) SubscribeAccountEvents(_ context.Context) (<-chan *AccountEvent, func(), error) {
	ch := make(chan *AccountEvent, 16)
	b.mu.Lock()
	b.accountSubs[ch] = struct{}{}
	b.mu.Unlock()
	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.accountSubs, ch)
		close(ch)
	}
	return ch, unsub, nil
}

// RedisBus is a Redis pub/sub fan-out bus.
type RedisBus struct {
	client *redis.Client
}

// NewRedisBus wraps client.
func NewRedisBus(client *redis.Client) *RedisBus {
	return &RedisBus{client: client}
}

// Publish persists nothing; it only fans out (persistence is the caller's job).
func (b *RedisBus) Publish(ctx context.Context, n *models.Notification) error {
	payload, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("bus: marshal: %w", err)
	}
	if err := b.client.Publish(ctx, channelFor(n.UserID), payload).Err(); err != nil {
		return fmt.Errorf("bus: publish: %w", err)
	}
	return nil
}

// Subscribe returns a channel fed from the user's Redis channel.
func (b *RedisBus) Subscribe(ctx context.Context, userID string) (<-chan *models.Notification, func(), error) {
	sub := b.client.Subscribe(ctx, channelFor(userID))
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, nil, fmt.Errorf("bus: subscribe: %w", err)
	}
	out := make(chan *models.Notification, 16)
	done := make(chan struct{})
	go func() {
		defer close(out)
		defer func() { _ = sub.Close() }()
		msgCh := sub.Channel()
		for {
			select {
			case <-done:
				return
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				var n models.Notification
				if err := json.Unmarshal([]byte(msg.Payload), &n); err != nil {
					continue
				}
				select {
				case out <- &n:
				case <-done:
					return
				}
			}
		}
	}()
	unsub := func() {
		select {
		case <-done:
		default:
			close(done)
		}
	}
	return out, unsub, nil
}

// PublishAccountEvent publishes an account revocation event to the shared account:events channel.
func (b *RedisBus) PublishAccountEvent(ctx context.Context, evt *AccountEvent) error {
	payload, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("bus: marshal account event: %w", err)
	}
	if err := b.client.Publish(ctx, accountEventsChannel, payload).Err(); err != nil {
		return fmt.Errorf("bus: publish account event: %w", err)
	}
	return nil
}

// SubscribeAccountEvents subscribes once to the shared account:events Redis channel.
func (b *RedisBus) SubscribeAccountEvents(ctx context.Context) (<-chan *AccountEvent, func(), error) {
	sub := b.client.Subscribe(ctx, accountEventsChannel)
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, nil, fmt.Errorf("bus: subscribe account events: %w", err)
	}
	out := make(chan *AccountEvent, 16)
	done := make(chan struct{})
	go func() {
		defer close(out)
		defer func() { _ = sub.Close() }()
		msgCh := sub.Channel()
		for {
			select {
			case <-done:
				return
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				var evt AccountEvent
				if err := json.Unmarshal([]byte(msg.Payload), &evt); err != nil {
					continue
				}
				select {
				case out <- &evt:
				case <-done:
					return
				}
			}
		}
	}()
	unsub := func() {
		select {
		case <-done:
		default:
			close(done)
		}
	}
	return out, unsub, nil
}
