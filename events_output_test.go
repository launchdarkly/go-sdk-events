package ldevents

import (
	"encoding/json"
	"testing"

	"gopkg.in/launchdarkly/go-sdk-common.v2/ldreason"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldtime"
	"gopkg.in/launchdarkly/go-sdk-common.v2/lduser"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldvalue"

	m "github.com/launchdarkly/go-test-helpers/v2/matchers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fakeTime = ldtime.UnixMillisecondTime(100000)

var (
	withoutReasons = NewEventFactory(false, fakeTimeFn)
	withReasons    = NewEventFactory(true, fakeTimeFn)
)

func fakeTimeFn() ldtime.UnixMillisecondTime { return fakeTime }

func TestEventOutputFullEvents(t *testing.T) {
	userKey := "u"
	user := User(lduser.NewUser(userKey))
	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 100}

	defaultFormatter := eventOutputFormatter{config: basicConfigWithoutPrivateAttrs()}

	userJSON := json.RawMessage(`{"key":"u"}`)

	t.Run("feature", func(t *testing.T) {
		event1 := withoutReasons.NewEvalEvent(flag, user, ldreason.NewEvaluationDetail(ldvalue.String("v"), 1, noReason),
			ldvalue.String("dv"), "")
		verifyEventOutput(t, defaultFormatter, event1,
			m.JSONEqual(map[string]interface{}{
				"kind":         "feature",
				"creationDate": fakeTime,
				"key":          flag.Key,
				"version":      flag.Version,
				"userKey":      userKey,
				"variation":    1,
				"value":        "v",
				"default":      "dv",
			}))

		event1r := withReasons.NewEvalEvent(flag, user,
			ldreason.NewEvaluationDetail(ldvalue.String("v"), 1, ldreason.NewEvalReasonFallthrough()),
			ldvalue.String("dv"), "")
		verifyEventOutput(t, defaultFormatter, event1r,
			m.JSONEqual(map[string]interface{}{
				"kind":         "feature",
				"creationDate": fakeTime,
				"key":          flag.Key,
				"version":      flag.Version,
				"userKey":      userKey,
				"variation":    1,
				"value":        "v",
				"default":      "dv",
				"reason":       json.RawMessage(`{"kind":"FALLTHROUGH"}`),
			}))

		event2 := withoutReasons.NewEvalEvent(flag, user, ldreason.EvaluationDetail{Value: ldvalue.String("v")},
			ldvalue.String("dv"), "")
		event2.Variation = ldvalue.OptionalInt{}
		verifyEventOutput(t, defaultFormatter, event2,
			m.JSONEqual(map[string]interface{}{
				"kind":         "feature",
				"creationDate": fakeTime,
				"key":          flag.Key,
				"version":      flag.Version,
				"userKey":      userKey,
				"value":        "v",
				"default":      "dv",
			}))

		event3 := withoutReasons.NewEvalEvent(flag, user, ldreason.NewEvaluationDetail(ldvalue.String("v"), 1, noReason),
			ldvalue.String("dv"), "pre")
		verifyEventOutput(t, defaultFormatter, event3,
			m.JSONEqual(map[string]interface{}{
				"kind":         "feature",
				"creationDate": fakeTime,
				"key":          flag.Key,
				"version":      flag.Version,
				"userKey":      userKey,
				"variation":    1,
				"value":        "v",
				"default":      "dv",
				"prereqOf":     "pre",
			}))

		event4 := withoutReasons.NewUnknownFlagEvent("flagkey", user,
			ldvalue.String("dv"), ldreason.EvaluationReason{})
		verifyEventOutput(t, defaultFormatter, event4,
			m.JSONEqual(map[string]interface{}{
				"kind":         "feature",
				"creationDate": fakeTime,
				"key":          flag.Key,
				"userKey":      userKey,
				"value":        "dv",
				"default":      "dv",
			}))

		event5 := withoutReasons.NewUnknownFlagEvent("flagkey", User(lduser.NewAnonymousUser("u")),
			ldvalue.String("dv"), ldreason.EvaluationReason{})
		verifyEventOutput(t, defaultFormatter, event5,
			m.JSONEqual(map[string]interface{}{
				"kind":         "feature",
				"creationDate": fakeTime,
				"key":          flag.Key,
				"userKey":      userKey,
				"contextKind":  "anonymousUser",
				"value":        "dv",
				"default":      "dv",
			}))
	})

	t.Run("debug", func(t *testing.T) {
		event1 := withoutReasons.NewEvalEvent(flag, user, ldreason.NewEvaluationDetail(ldvalue.String("v"), 1, noReason),
			ldvalue.String("dv"), "")
		event1.Debug = true
		verifyEventOutput(t, defaultFormatter, event1,
			m.JSONEqual(map[string]interface{}{
				"kind":         "debug",
				"creationDate": fakeTime,
				"key":          flag.Key,
				"version":      flag.Version,
				"user":         userJSON,
				"variation":    1,
				"value":        "v",
				"default":      "dv",
			}))
	})

	t.Run("identify", func(t *testing.T) {
		event := withoutReasons.NewIdentifyEvent(user)
		verifyEventOutput(t, defaultFormatter, event,
			m.JSONEqual(map[string]interface{}{
				"kind":         "identify",
				"creationDate": fakeTime,
				"key":          userKey,
				"user":         userJSON,
			}))
	})

	t.Run("custom", func(t *testing.T) {
		event1 := withoutReasons.NewCustomEvent("eventkey", user, ldvalue.Null(), false, 0)
		verifyEventOutput(t, defaultFormatter, event1,
			m.JSONEqual(map[string]interface{}{
				"kind":         "custom",
				"key":          "eventkey",
				"creationDate": fakeTime,
				"userKey":      userKey,
			}))

		event2 := withoutReasons.NewCustomEvent("eventkey", user, ldvalue.String("d"), false, 0)
		verifyEventOutput(t, defaultFormatter, event2,
			m.JSONEqual(map[string]interface{}{
				"kind":         "custom",
				"key":          "eventkey",
				"creationDate": fakeTime,
				"userKey":      userKey,
				"data":         "d",
			}))

		event3 := withoutReasons.NewCustomEvent("eventkey", user, ldvalue.Null(), true, 2.5)
		verifyEventOutput(t, defaultFormatter, event3,
			m.JSONEqual(map[string]interface{}{
				"kind":         "custom",
				"key":          "eventkey",
				"creationDate": fakeTime,
				"userKey":      userKey,
				"metricValue":  2.5,
			}))

		event4 := withoutReasons.NewCustomEvent("eventkey", User(lduser.NewAnonymousUser("u")), ldvalue.Null(), false, 0)
		verifyEventOutput(t, defaultFormatter, event4,
			m.JSONEqual(map[string]interface{}{
				"kind":         "custom",
				"key":          "eventkey",
				"creationDate": fakeTime,
				"userKey":      userKey,
				"contextKind":  "anonymousUser",
			}))
	})

	t.Run("index", func(t *testing.T) {
		event := indexEvent{BaseEvent: BaseEvent{CreationDate: fakeTime, User: user}}
		verifyEventOutput(t, defaultFormatter, event,
			m.JSONEqual(map[string]interface{}{
				"kind":         "index",
				"creationDate": fakeTime,
				"user":         userJSON,
			}))
	})

	t.Run("raw", func(t *testing.T) {
		rawData := json.RawMessage(`{"kind":"alias","arbitrary":["we","don't","care","what's","in","here"]}`)
		event := rawEvent{data: rawData}
		verifyEventOutput(t, defaultFormatter, event, m.JSONEqual(rawData))
	})
}

func TestEventOutputSummaryEvents(t *testing.T) {
	user := User(lduser.NewUser("u"))
	flag1v1 := flagEventPropertiesImpl{Key: "flag1", Version: 100}
	flag1v2 := flagEventPropertiesImpl{Key: "flag1", Version: 200}
	flag1Default := ldvalue.String("default1")
	flag2 := flagEventPropertiesImpl{Key: "flag2", Version: 1}
	flag2Default := ldvalue.String("default2")

	defaultFormatter := eventOutputFormatter{config: basicConfigWithoutPrivateAttrs()}

	t.Run("summary - single flag, single counter", func(t *testing.T) {
		es1 := newEventSummarizer()
		event1 := withoutReasons.NewEvalEvent(flag1v1, user, ldreason.NewEvaluationDetail(ldvalue.String("v"), 1, noReason),
			ldvalue.String("dv"), "")
		es1.summarizeEvent(event1)
		verifySummaryEventOutput(t, defaultFormatter, es1.snapshot(),
			m.JSONEqual(map[string]interface{}{
				"kind":      "summary",
				"startDate": fakeTime,
				"endDate":   fakeTime,
				"features": map[string]interface{}{
					"flag1": map[string]interface{}{
						"counters": json.RawMessage(`[{"count":1,"value":"v","variation":1,"version":100}]`),
						"default":  "dv",
					},
				},
			}))

		es2 := newEventSummarizer()
		event2 := withoutReasons.NewEvalEvent(flag1v1, user, ldreason.EvaluationDetail{Value: ldvalue.String("dv")},
			ldvalue.String("dv"), "")
		event2.Variation = ldvalue.OptionalInt{}
		es2.summarizeEvent(event2)
		verifySummaryEventOutput(t, defaultFormatter, es2.snapshot(),
			m.JSONEqual(map[string]interface{}{
				"kind":      "summary",
				"startDate": fakeTime,
				"endDate":   fakeTime,
				"features": map[string]interface{}{
					"flag1": map[string]interface{}{
						"counters": json.RawMessage(`[{"count":1,"value":"dv","version":100}]`),
						"default":  "dv",
					},
				},
			}))

		es3 := newEventSummarizer()
		event3 := withoutReasons.NewUnknownFlagEvent("flagkey", user,
			ldvalue.String("dv"), ldreason.EvaluationReason{})
		es3.summarizeEvent(event3)
		verifySummaryEventOutput(t, defaultFormatter, es3.snapshot(),
			m.JSONEqual(map[string]interface{}{
				"kind":      "summary",
				"startDate": fakeTime,
				"endDate":   fakeTime,
				"features": map[string]interface{}{
					"flagkey": map[string]interface{}{
						"counters": json.RawMessage(`[{"count":1,"value":"dv","unknown":true}]`),
						"default":  "dv",
					},
				},
			}))
	})

	t.Run("summary - multiple counters", func(t *testing.T) {
		es := newEventSummarizer()
		es.summarizeEvent(withoutReasons.NewEvalEvent(flag1v1, user, ldreason.NewEvaluationDetail(ldvalue.String("a"), 1, noReason),
			flag1Default, ""))
		es.summarizeEvent(withoutReasons.NewEvalEvent(flag1v1, user, ldreason.NewEvaluationDetail(ldvalue.String("b"), 2, noReason),
			flag1Default, ""))
		es.summarizeEvent(withoutReasons.NewEvalEvent(flag1v1, user, ldreason.NewEvaluationDetail(ldvalue.String("a"), 1, noReason),
			flag1Default, ""))
		es.summarizeEvent(withoutReasons.NewEvalEvent(flag1v2, user, ldreason.NewEvaluationDetail(ldvalue.String("a"), 1, noReason),
			flag1Default, ""))
		es.summarizeEvent(withoutReasons.NewEvalEvent(flag2, user, ldreason.NewEvaluationDetail(ldvalue.String("c"), 3, noReason),
			flag2Default, ""))

		bytes, count := defaultFormatter.makeOutputEvents(nil, es.snapshot())
		require.Equal(t, 1, count)

		// Using a nested matcher expression here, rather than an equality assertion on the whole JSON object,
		// because the ordering of array items in "counters" is indeterminate so we need m.ItemsInAnyOrder().
		m.In(t).Assert(bytes, m.JSONArray().Should(m.Items(
			m.MapOf(
				m.KV("kind", m.Equal("summary")),
				m.KV("startDate", m.Not(m.BeNil())),
				m.KV("endDate", m.Not(m.BeNil())),
				m.KV("features", m.MapOf(
					m.KV("flag1", m.MapOf(
						m.KV("default", m.JSONEqual(flag1Default)),
						m.KV("counters", m.ItemsInAnyOrder(
							m.JSONStrEqual(`{"version":100,"variation":1,"value":"a","count":2}`),
							m.JSONStrEqual(`{"version":100,"variation":2,"value":"b","count":1}`),
							m.JSONStrEqual(`{"version":200,"variation":1,"value":"a","count":1}`),
						)),
					)),
					m.KV("flag2", m.MapOf(
						m.KV("default", m.JSONEqual(flag2Default)),
						m.KV("counters", m.ItemsInAnyOrder(
							m.JSONStrEqual(`{"version":1,"variation":3,"value":"c","count":1}`),
						)),
					)),
				)),
			),
		)))
	})

	t.Run("empty payload", func(t *testing.T) {
		bytes, count := defaultFormatter.makeOutputEvents([]commonEvent{}, eventSummary{})
		assert.Nil(t, bytes)
		assert.Equal(t, 0, count)
	})
}

func verifyEventOutput(t *testing.T, formatter eventOutputFormatter, event commonEvent, jsonMatcher m.Matcher) {
	t.Helper()
	bytes, count := formatter.makeOutputEvents([]commonEvent{event}, eventSummary{})
	require.Equal(t, 1, count)
	m.In(t).Assert(bytes, m.JSONArray().Should(m.Items(jsonMatcher)))
}

func verifySummaryEventOutput(t *testing.T, formatter eventOutputFormatter, summary eventSummary, jsonMatcher m.Matcher) {
	t.Helper()
	bytes, count := formatter.makeOutputEvents(nil, summary)
	require.Equal(t, 1, count)
	m.In(t).Assert(bytes, m.JSONArray().Should(m.Items(jsonMatcher)))
}
