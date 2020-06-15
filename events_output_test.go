package ldevents

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gopkg.in/launchdarkly/go-sdk-common.v2/ldreason"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldtime"
	"gopkg.in/launchdarkly/go-sdk-common.v2/lduser"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldvalue"
)

func TestEventOutput(t *testing.T) {
	fakeTime := ldtime.UnixMillisecondTime(100000)
	timeFn := func() ldtime.UnixMillisecondTime { return fakeTime }
	withoutReasons := NewEventFactory(false, timeFn)
	withReasons := NewEventFactory(true, timeFn)
	user := User(lduser.NewUser("u"))
	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 100}

	defaultFormatter := eventOutputFormatter{config: epDefaultConfig}
	formatterWithInlineUsers := eventOutputFormatter{config: epDefaultConfig}
	formatterWithInlineUsers.config.InlineUsersInEvents = true

	t.Run("feature", func(t *testing.T) {
		event1 := withoutReasons.NewSuccessfulEvalEvent(flag, user, 1, ldvalue.String("v"),
			ldvalue.String("dv"), ldreason.EvaluationReason{}, "")
		verifyEventOutput(t, defaultFormatter, event1,
			`{"kind":"feature","creationDate":100000,"key":"flagkey","version":100,"userKey":"u","variation":1,"value":"v","default":"dv","reason":null}`)
		verifyEventOutput(t, formatterWithInlineUsers, event1,
			`{"kind":"feature","creationDate":100000,"key":"flagkey","version":100,"user":{"key":"u"},"variation":1,"value":"v","default":"dv","reason":null}`)

		event1r := withReasons.NewSuccessfulEvalEvent(flag, user, 1, ldvalue.String("v"),
			ldvalue.String("dv"), ldreason.NewEvalReasonFallthrough(), "")
		verifyEventOutput(t, defaultFormatter, event1r,
			`{"kind":"feature","creationDate":100000,"key":"flagkey","version":100,"userKey":"u","variation":1,"value":"v","default":"dv","reason":{"kind":"FALLTHROUGH"}}`)

		event2 := withoutReasons.NewSuccessfulEvalEvent(flag, user, -1, ldvalue.String("v"),
			ldvalue.String("dv"), ldreason.EvaluationReason{}, "")
		verifyEventOutput(t, defaultFormatter, event2,
			`{"kind":"feature","creationDate":100000,"key":"flagkey","version":100,"userKey":"u","value":"v","default":"dv","reason":null}`)

		event3 := withoutReasons.NewSuccessfulEvalEvent(flag, user, 1, ldvalue.String("v"),
			ldvalue.String("dv"), ldreason.EvaluationReason{}, "pre")
		verifyEventOutput(t, defaultFormatter, event3,
			`{"kind":"feature","creationDate":100000,"key":"flagkey","version":100,"userKey":"u","variation":1,"value":"v","default":"dv","reason":null,"prereqOf":"pre"}`)

		event4 := withoutReasons.NewUnknownFlagEvent("flagkey", user,
			ldvalue.String("dv"), ldreason.EvaluationReason{})
		verifyEventOutput(t, defaultFormatter, event4,
			`{"kind":"feature","creationDate":100000,"key":"flagkey","userKey":"u","value":"dv","default":"dv","reason":null}`)
	})

	t.Run("debug", func(t *testing.T) {
		event1 := withoutReasons.NewSuccessfulEvalEvent(flag, user, 1, ldvalue.String("v"),
			ldvalue.String("dv"), ldreason.EvaluationReason{}, "")
		event1.Debug = true
		verifyEventOutput(t, defaultFormatter, event1,
			`{"kind":"debug","creationDate":100000,"key":"flagkey","version":100,"user":{"key":"u"},"variation":1,"value":"v","default":"dv","reason":null}`)
	})

	t.Run("identify", func(t *testing.T) {
		event := withoutReasons.NewIdentifyEvent(user)
		verifyEventOutput(t, defaultFormatter, event,
			`{"kind":"identify","creationDate":100000,"key":"u","user":{"key":"u"}}`)
	})

	t.Run("custom", func(t *testing.T) {
		event1 := withoutReasons.NewCustomEvent("eventkey", user, ldvalue.Null(), false, 0)
		verifyEventOutput(t, defaultFormatter, event1,
			`{"kind":"custom","creationDate":100000,"key":"eventkey","userKey":"u","data":null}`)
		verifyEventOutput(t, formatterWithInlineUsers, event1,
			`{"kind":"custom","creationDate":100000,"key":"eventkey","user":{"key":"u"},"data":null}`)

		event2 := withoutReasons.NewCustomEvent("eventkey", user, ldvalue.String("d"), false, 0)
		verifyEventOutput(t, defaultFormatter, event2,
			`{"kind":"custom","creationDate":100000,"key":"eventkey","userKey":"u","data":"d"}`)

		event3 := withoutReasons.NewCustomEvent("eventkey", user, ldvalue.Null(), true, 2.5)
		verifyEventOutput(t, defaultFormatter, event3,
			`{"kind":"custom","creationDate":100000,"key":"eventkey","userKey":"u","data":null,"metricValue":2.5}`)
	})

	t.Run("index", func(t *testing.T) {
		event := IndexEvent{BaseEvent: BaseEvent{CreationDate: fakeTime, User: user}}
		verifyEventOutput(t, defaultFormatter, event,
			`{"kind":"index","creationDate":100000,"user":{"key":"u"}}`)
	})

	t.Run("summary", func(t *testing.T) {
		es1 := newEventSummarizer()
		event1 := withoutReasons.NewSuccessfulEvalEvent(flag, user, 1, ldvalue.String("v"),
			ldvalue.String("dv"), ldreason.EvaluationReason{}, "")
		es1.summarizeEvent(event1)
		verifySummaryEventOutput(t, defaultFormatter, es1.snapshot(),
			`{"kind":"summary","startDate":100000,"endDate":100000,"features":{"flagkey":{"counters":[{"count":1,"value":"v","variation":1,"version":100}],"default":"dv"}}}`)

		es2 := newEventSummarizer()
		event2 := withoutReasons.NewSuccessfulEvalEvent(flag, user, -1, ldvalue.String("dv"),
			ldvalue.String("dv"), ldreason.EvaluationReason{}, "")
		es2.summarizeEvent(event2)
		verifySummaryEventOutput(t, defaultFormatter, es2.snapshot(),
			`{"kind":"summary","startDate":100000,"endDate":100000,"features":{"flagkey":{"counters":[{"count":1,"value":"dv","version":100}],"default":"dv"}}}`)

		es3 := newEventSummarizer()
		event3 := withoutReasons.NewUnknownFlagEvent("flagkey", user,
			ldvalue.String("dv"), ldreason.EvaluationReason{})
		es3.summarizeEvent(event3)
		verifySummaryEventOutput(t, defaultFormatter, es3.snapshot(),
			`{"kind":"summary","startDate":100000,"endDate":100000,"features":{"flagkey":{"counters":[{"count":1,"value":"dv","unknown":true}],"default":"dv"}}}`)
	})
}

func verifyEventOutput(t *testing.T, formatter eventOutputFormatter, event Event, expectedJSON string) {
	var expectedValue ldvalue.Value
	require.NoError(t, json.Unmarshal([]byte(expectedJSON), &expectedValue))
	outJSON := formatter.makeOutputEvents([]Event{event}, eventSummary{})
	var outValue ldvalue.Value
	require.Len(t, outJSON, 1)
	_ = json.Unmarshal(outJSON[0], &outValue)
	assert.Equal(t, expectedValue, outValue, "expected JSON: %s, actual JSON: %s", expectedJSON, outJSON[0])
}

func verifySummaryEventOutput(t *testing.T, formatter eventOutputFormatter, summary eventSummary, expectedJSON string) {
	var expectedValue ldvalue.Value
	require.NoError(t, json.Unmarshal([]byte(expectedJSON), &expectedValue))
	outJSON := formatter.makeOutputEvents(nil, summary)
	var outValue ldvalue.Value
	require.Len(t, outJSON, 1)
	_ = json.Unmarshal(outJSON[0], &outValue)
	assert.Equal(t, expectedValue, outValue, "expected JSON: %s, actual JSON: %s", expectedJSON, outJSON[0])
}
