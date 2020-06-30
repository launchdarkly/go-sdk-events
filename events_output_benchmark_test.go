package ldevents

import (
	"testing"

	"gopkg.in/launchdarkly/go-sdk-common.v2/ldreason"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldtime"
	"gopkg.in/launchdarkly/go-sdk-common.v2/lduser"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldvalue"
)

var (
	benchmarkBytesResult []byte
	benchmarkIntResult   int
)

func BenchmarkEventOutputFormatterBasicEvents(b *testing.B) {
	events := makeBasicEvents()
	ef := eventOutputFormatter{}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchmarkBytesResult, _ = ef.makeOutputEvents(events, eventSummary{})
	}
}

func BenchmarkEventOutputFormatterBasicEventsWithPrivateAttributes(b *testing.B) {
	events := makeBasicEvents()
	ef := eventOutputFormatter{
		userFilter: userFilter{
			globalPrivateAttributes: []lduser.UserAttribute{
				lduser.NameAttribute,
				lduser.UserAttribute("custom-attr"),
			},
		},
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchmarkBytesResult, _ = ef.makeOutputEvents(events, eventSummary{})
	}
}

func makeBasicEvents() []Event {
	baseEvent := BaseEvent{
		CreationDate: ldtime.UnixMillisNow(),
		User: EventUser{
			User: lduser.NewUserBuilder("user-key").
				Email("test@example.com").
				Name("user-name").
				Custom("custom-attr", ldvalue.Bool(true)).
				Build(),
		},
	}
	return []Event{
		FeatureRequestEvent{
			BaseEvent: baseEvent,
			Key:       "flag1",
			Variation: 1,
			Value:     ldvalue.Bool(true),
			Default:   ldvalue.Bool(false),
			Reason:    ldreason.NewEvalReasonFallthrough(),
			Version:   10,
		},
		CustomEvent{
			BaseEvent:   baseEvent,
			Key:         "event1",
			Data:        ldvalue.String("data"),
			HasMetric:   true,
			MetricValue: 1234,
		},
		IdentifyEvent{BaseEvent: baseEvent},
		indexEvent{BaseEvent: baseEvent},
	}
}

func BenchmarkEventOutputSummaryMultipleCounters(b *testing.B) {
	user := User(lduser.NewUser("u"))
	flag1v1 := flagEventPropertiesImpl{Key: "flag1", Version: 100}
	flag1v2 := flagEventPropertiesImpl{Key: "flag1", Version: 200}
	flag1Default := ldvalue.String("default1")
	flag2 := flagEventPropertiesImpl{Key: "flag2", Version: 1}
	flag2Default := ldvalue.String("default2")
	factory := NewEventFactory(false, fakeTimeFn)

	ef := eventOutputFormatter{config: epDefaultConfig}

	es := newEventSummarizer()
	es.summarizeEvent(factory.NewSuccessfulEvalEvent(flag1v1, user, 1, ldvalue.String("a"),
		flag1Default, noReason, ""))
	es.summarizeEvent(factory.NewSuccessfulEvalEvent(flag1v1, user, 2, ldvalue.String("b"),
		flag1Default, noReason, ""))
	es.summarizeEvent(factory.NewSuccessfulEvalEvent(flag1v1, user, 1, ldvalue.String("a"),
		flag1Default, noReason, ""))
	es.summarizeEvent(factory.NewSuccessfulEvalEvent(flag1v2, user, 1, ldvalue.String("a"),
		flag1Default, noReason, ""))
	es.summarizeEvent(factory.NewSuccessfulEvalEvent(flag2, user, 3, ldvalue.String("c"),
		flag2Default, noReason, ""))
	summary := es.snapshot()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchmarkBytesResult, _ = ef.makeOutputEvents(nil, summary)
	}
}
