// Package events implements an in-process pub/sub broker used to push
// real-time inbox updates from LINE webhook handlers + admin actions to
// SSE-connected admin clients (browsers).
//
// Why in-process: Nexflow runs as a single Go container (one process per
// deployment). All subscribers share the same memory; a sync.Map of channels
// is enough — no Redis/NATS needed. If the deployment ever scales to
// multiple replicas, swap this for a Redis-backed broker (interface stays
// the same).
//
// Why not WebSocket: SSE is one-way (server → client) which is exactly what
// the inbox needs — admin sends via existing POST endpoints. EventSource has
// auto-reconnect built into every browser, so the frontend stays simple.
package events

import (
	"sync"
	"sync/atomic"
)

// Event types — keep these short and stable; they appear in the SSE
// `event:` line and the frontend dispatches on them. Add new types as
// needed but don't rename without coordinating with the FE handlers.
const (
	// MessageReceived — a new chat_message row was inserted (incoming OR
	// outgoing). Payload includes line_user_id + the full ChatMessage so
	// the inbox/thread can render without an extra fetch.
	TypeMessageReceived = "message_received"

	// ConversationUpdated — a metadata field on chat_conversations changed
	// (status / phone / unread_count / display_name / picture / tags).
	// Payload includes line_user_id and the changed fields.
	TypeConversationUpdated = "conversation_updated"

	// UnreadChanged — total unread count across all conversations changed
	// (used by the sidebar badge). Payload: { total: int }.
	TypeUnreadChanged = "unread_changed"

	// NotificationCreated — a user-facing in-app notification was inserted.
	// Payload includes notification + unread_count.
	TypeNotificationCreated = "notification_created"

	// NotificationUnreadChanged — current user's notification unread count
	// changed. Payload: { total: int }.
	TypeNotificationUnreadChanged = "notification_unread_changed"

	// ShopeeRealtimeChanged — Shopee realtime snapshots changed and clients
	// should refresh local dashboard/query state.
	TypeShopeeRealtimeChanged = "shopee_realtime_changed"
)

// Event is what the broker fans out. Keep Payload as a generic map so
// handlers can attach whatever they need without forcing a typed schema.
type Event struct {
	Type         string         `json:"-"` // never appears in JSON; goes in SSE event: line
	TargetUserID string         `json:"-"` // optional; when set, SSE only delivers to that user
	Payload      map[string]any `json:"payload"`
}

// subscriber holds one admin's connection — a buffered channel so a slow
// consumer doesn't block other subscribers, and a unique id so Unsubscribe
// can find this exact subscriber even when the same admin opens multiple
// tabs (each gets its own subscriber).
type subscriber struct {
	id uint64
	ch chan Event
}

// Broker is a tiny fan-out hub. Safe for concurrent Subscribe/Unsubscribe/
// Publish from any goroutine — protected by a single RWMutex.
//
// We don't filter by adminID at the broker level: every subscriber gets
// every event. Admin-only filtering is unnecessary because all events are
// already admin-relevant (chat metadata + LINE messages). If we later add
// per-admin events (e.g. "this assignment is yours") we'll add a target
// field to Event and filter in the SSE handler.
type Broker struct {
	mu     sync.RWMutex
	subs   map[uint64]*subscriber
	nextID atomic.Uint64
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[uint64]*subscriber)}
}

// Subscribe returns a receive-only channel for events plus a cleanup func.
// Always defer the cleanup — leaking subscribers grows the fan-out forever.
//
// Buffer size of 16 is enough for normal load (admin opening a thread that
// briefly bursts events). If a consumer is so slow the channel fills, we
// drop the event for that subscriber rather than blocking Publish (better
// to lose a UI update than wedge the webhook handler).
func (b *Broker) Subscribe() (<-chan Event, func()) {
	id := b.nextID.Add(1)
	s := &subscriber{id: id, ch: make(chan Event, 16)}

	b.mu.Lock()
	b.subs[id] = s
	b.mu.Unlock()

	cleanup := func() {
		b.mu.Lock()
		if existing, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(existing.ch)
		}
		b.mu.Unlock()
	}
	return s.ch, cleanup
}

// Publish fans the event out to every current subscriber. Non-blocking:
// if a subscriber's buffer is full, drop the event for that subscriber
// (the next event will go through; a missed UI update is recoverable —
// the worst case is the inbox is briefly stale until the next event).
func (b *Broker) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, s := range b.subs {
		select {
		case s.ch <- ev:
		default:
			// subscriber buffer full → drop this event. Don't log here —
			// would spam under load. SSE handler heartbeats will eventually
			// trigger a reconnect if the client is truly stuck.
		}
	}
}

// SubscriberCount is exposed for /health style endpoints + tests.
func (b *Broker) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
