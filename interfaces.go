package ldevents

import (
	"encoding/json"
	"time"

	"github.com/launchdarkly/go-sdk-common/v4/ldtime"
)

// EventProcessor defines the interface for dispatching analytics events.
type EventProcessor interface {
	// RecordEvaluation records evaluation information asynchronously. Depending on the feature
	// flag properties and event properties, this may be transmitted to the events service as an
	// individual event, or may only be added into summary data.
	RecordEvaluation(EvaluationData)

	// RecordIdentifyEvent records an identify event asynchronously.
	RecordIdentifyEvent(IdentifyEventData)

	// RecordCustomEvent records a custom event asynchronously.
	RecordCustomEvent(CustomEventData)

	// RecordMigrationOpEvent records a migration operation event asynchronously.
	RecordMigrationOpEvent(MigrationOpEventData)

	// RecordRawEvent adds an event to the output buffer that is not parsed or transformed in any way.
	// This is used by the Relay Proxy when forwarding events.
	RecordRawEvent(data json.RawMessage)

	// Flush specifies that any buffered events should be sent as soon as possible, rather than waiting
	// for the next flush interval. This method is asynchronous, so events still may not be sent
	// until a later time.
	Flush()

	// FlushBlocking attempts to flush any buffered events, blocking until either they have been
	// successfully delivered or delivery has failed. If there were no buffered events, it returns true
	// immediately. The timeout parameter, if non-zero, specifies the maximum amount of time to wait
	// before the method will return; a timeout does not stop the event processor from continuing to
	// try to deliver the events in the background, if applicable. The method returns true on completion
	// or false if timed out.
	FlushBlocking(timeout time.Duration) bool

	// Close shuts down all event processor activity, after first ensuring that all events have been
	// delivered. Subsequent calls to SendEvent() or Flush() will be ignored.
	Close() error
}

// EventSender defines the interface for delivering already-formatted analytics event data to the events service.
type EventSender interface {
	// SendEventData attempts to deliver an event data payload.
	SendEventData(kind EventDataKind, data []byte, eventCount int) EventSenderResult
}

// EventDataKind is a parameter passed to EventSender to indicate the type of event data payload.
type EventDataKind string

const (
	// AnalyticsEventDataKind denotes a payload of analytics event data.
	AnalyticsEventDataKind EventDataKind = "analytics"
	// DiagnosticEventDataKind denotes a payload of diagnostic event data.
	DiagnosticEventDataKind EventDataKind = "diagnostic"
)

// EventMetrics defines an interface for receiving metrics about event processing. Implementations
// can use this to record telemetry (e.g. via OpenTelemetry) about events that are dropped due to
// capacity limits or successfully sent. If no implementation is provided in EventsConfiguration,
// no metrics are recorded.
type EventMetrics interface {
	// RecordDroppedEvents is called when events are discarded because the event buffer has reached
	// its configured capacity. The count parameter indicates how many events were dropped.
	RecordDroppedEvents(count int)

	// RecordEventsSent is called when a batch of events has been successfully delivered to the
	// events service. The count parameter indicates how many events were in the batch.
	RecordEventsSent(count int)

	// RecordEventsFailedSend is called when a batch of events could not be delivered to the
	// events service after all retry attempts. The count parameter indicates how many events
	// were in the failed batch. The metadata parameter provides additional context about the failure.
	RecordEventsFailedSend(count int, metadata EventSendFailureMetadata)

	// RecordEventsBytesSent is called when a batch of events has been successfully delivered.
	// The bytes parameter is the size of the serialized event payload before compression.
	RecordEventsBytesSent(bytes int)

	// RecordPendingEvents is called after any operation that changes the number of events
	// buffered in the outbox. The count parameter is the current total number of events pending.
	RecordPendingEvents(count int)
}

// NoOpEventMetrics is a default implementation of EventMetrics that does nothing.
// It is used when no EventMetrics is provided in EventsConfiguration, eliminating the
// need for nil checks at every call site.
type NoOpEventMetrics struct{}

func (NoOpEventMetrics) RecordDroppedEvents(int)                              {}
func (NoOpEventMetrics) RecordEventsSent(int)                                 {}
func (NoOpEventMetrics) RecordEventsFailedSend(int, EventSendFailureMetadata) {}
func (NoOpEventMetrics) RecordEventsBytesSent(int)                            {}
func (NoOpEventMetrics) RecordPendingEvents(int)                              {}

// EventSendFailureMetadata provides additional context about why an event batch failed to send.
// This struct may be extended with additional fields in the future without breaking compatibility.
type EventSendFailureMetadata struct {
	// StatusCode is the HTTP status code returned by the events service, or 0 if the failure
	// occurred before receiving an HTTP response (e.g. network error).
	StatusCode int
}

// EventSenderResult is the return type for EventSender.SendEventData.
type EventSenderResult struct {
	// Success is true if the event payload was delivered.
	Success bool
	// MustShutDown is true if the server returned an error indicating that no further event data should be sent.
	// This normally means that the SDK key is invalid.
	MustShutDown bool
	// TimeFromServer is the last known date/time reported by the server, if available, otherwise zero.
	TimeFromServer ldtime.UnixMillisecondTime
	// StatusCode is the HTTP status code from the last response, or 0 if no response was received.
	StatusCode int
}
