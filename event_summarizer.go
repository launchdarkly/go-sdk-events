package ldevents

import (
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldcontext"
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldtime"
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldvalue"
)

// Manages the state of summarizable information for the EventProcessor, including the
// event counters and user deduplication. Note that the methods for this type are
// deliberately not thread-safe, because they should always be called from EventProcessor's
// single event-processing goroutine.
type eventSummarizer struct {
	eventsState eventSummary
}

type eventSummary struct {
	flags     map[string]flagSummary
	startDate ldtime.UnixMillisecondTime
	endDate   ldtime.UnixMillisecondTime
}

type flagSummary struct {
	counters     map[counterKey]*counterValue
	contextKinds map[ldcontext.Kind]struct{}
	defaultValue ldvalue.Value
}

type counterKey struct {
	variation ldvalue.OptionalInt
	version   ldvalue.OptionalInt
}

type counterValue struct {
	count     int
	flagValue ldvalue.Value
}

func newEventSummarizer() eventSummarizer {
	return eventSummarizer{eventsState: newEventSummary()}
}

func newEventSummary() eventSummary {
	return eventSummary{
		flags: make(map[string]flagSummary),
	}
}

func (s eventSummary) hasCounters() bool {
	return len(s.flags) != 0
}

// Adds this event to our counters.
func (s *eventSummarizer) summarizeEvent(fe FeatureRequestEvent) {
	flag, ok := s.eventsState.flags[fe.Key]
	if !ok {
		flag = flagSummary{
			counters:     make(map[counterKey]*counterValue),
			contextKinds: make(map[ldcontext.Kind]struct{}),
			defaultValue: fe.Default,
		}
		s.eventsState.flags[fe.Key] = flag
	}

	counterKey := counterKey{variation: fe.Variation, version: fe.Version}
	if value, ok := flag.counters[counterKey]; ok {
		value.count++
	} else {
		flag.counters[counterKey] = &counterValue{
			count:     1,
			flagValue: fe.Value,
		}
	}

	if fe.Context.context.Multiple() {
		for i := 0; i < fe.Context.context.MultiKindCount(); i++ {
			if mc, ok := fe.Context.context.MultiKindByIndex(i); ok {
				flag.contextKinds[mc.Kind()] = struct{}{}
			}
		}
	} else {
		flag.contextKinds[fe.Context.context.Kind()] = struct{}{}
	}

	creationDate := fe.CreationDate
	if s.eventsState.startDate == 0 || creationDate < s.eventsState.startDate {
		s.eventsState.startDate = creationDate
	}
	if creationDate > s.eventsState.endDate {
		s.eventsState.endDate = creationDate
	}
}

// Returns a snapshot of the current summarized event data.
func (s *eventSummarizer) snapshot() eventSummary {
	return s.eventsState
}

func (s *eventSummarizer) reset() {
	s.eventsState = newEventSummary()
}
