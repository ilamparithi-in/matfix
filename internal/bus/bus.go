package bus

// # Subscription types

// SubscriptionID uniquely identifies a registered subscriber.
type SubscriptionID string

// HandlerFunc is the callback invoked for each event delivered to a subscriber.
type HandlerFunc func(EventEnvelope)

// # Bus interface

// Bus is the internal pub/sub broker. All internal components communicate via
// Bus rather than calling each other directly.
//
// Publish is safe to call concurrently.
// Subscribe and Unsubscribe are safe to call concurrently.
type Bus interface {
	// Publish dispatches env to all subscribers registered for env.Type.
	// If a subscriber's buffer is full the event is dropped for that subscriber;
	// it does not block and does not affect other subscribers.
	Publish(env EventEnvelope)

	// Subscribe registers handler to receive events of the given type.
	// Returns a SubscriptionID that can be passed to Unsubscribe.
	Subscribe(eventType EventType, handler HandlerFunc) SubscriptionID

	// Unsubscribe removes the subscription and stops delivering events to the
	// associated handler. It is a no-op if id is not known.
	Unsubscribe(id SubscriptionID)
}
