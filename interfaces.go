package ldevents

import (
	"encoding/json"

	"github.com/launchdarkly/go-sdk-common/v3/ldtime"
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

	// RecordRawEvent adds an event to the output buffer that is not parsed or transformed in any way.
	// This is used by the Relay Proxy when forwarding events.
	RecordRawEvent(data json.RawMessage)

	// Flush specifies that any buffered events should be sent as soon as possible, rather than waiting
	// for the next flush interval. This method is asynchronous, so events still may not be sent
	// until a later time.
	Flush()

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

// EventSenderResult is the return type for EventSender.SendEventData.
type EventSenderResult struct {
	// Success is true if the event payload was delivered.
	Success bool
	// MustShutDown is true if the server returned an error indicating that no further event data should be sent.
	// This normally means that the SDK key is invalid.
	MustShutDown bool
	// TimeFromServer is the last known date/time reported by the server, if available, otherwise zero.
	TimeFromServer ldtime.UnixMillisecondTime
}
