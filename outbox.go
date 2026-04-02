package ldevents

import (
	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
)

type eventsOutbox struct {
	events           []anyEventOutput
	summarizer       eventSummarizer
	capacity         int
	capacityExceeded bool
	droppedEvents    int
	loggers          ldlog.Loggers
	eventMetrics     EventMetrics
}

func newEventsOutbox(capacity int, loggers ldlog.Loggers, eventMetrics EventMetrics) *eventsOutbox {
	return &eventsOutbox{
		events:       make([]anyEventOutput, 0, capacity),
		summarizer:   newEventSummarizer(),
		capacity:     capacity,
		loggers:      loggers,
		eventMetrics: eventMetrics,
	}
}

func (b *eventsOutbox) addEvent(event anyEventInput) {
	if len(b.events) >= b.capacity {
		if !b.capacityExceeded {
			b.capacityExceeded = true
			b.loggers.Warn("Exceeded event queue capacity. Increase capacity to avoid dropping events.")
		}
		b.droppedEvents++
		b.eventMetrics.RecordDroppedEvents(1)
		return
	}
	b.capacityExceeded = false
	b.events = append(b.events, event)
	b.eventMetrics.RecordPendingEvents(len(b.events))
}

func (b *eventsOutbox) addToSummary(ed EvaluationData) {
	b.summarizer.summarizeEvent(ed)
}

func (b *eventsOutbox) getPayload(completed chan struct{}) flushPayload {
	var copied []anyEventOutput
	if len(b.events) > 0 {
		copied = make([]anyEventOutput, len(b.events))
		copy(copied, b.events)
	}
	return flushPayload{
		events:    copied,
		summary:   b.summarizer.snapshot(),
		completed: completed,
	}
}

func (b *eventsOutbox) clear() {
	for i := range b.events {
		b.events[i] = nil
	}
	b.events = b.events[0:0]
	b.summarizer.reset()
	b.eventMetrics.RecordPendingEvents(0)
}
