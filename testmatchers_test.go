package ldevents

import (
	"encoding/json"

	"gopkg.in/launchdarkly/go-sdk-common.v3/ldcontext"
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldreason"
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldtime"
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldvalue"

	"github.com/launchdarkly/go-test-helpers/v2/jsonhelpers"
	m "github.com/launchdarkly/go-test-helpers/v2/matchers"
)

func equalNumericTime(unixTime ldtime.UnixMillisecondTime) m.Matcher {
	// To avoid problems with mismatch of numeric types, it's simplest to use JSONEqual which compares as a JSON number
	return m.JSONEqual(unixTime)
}

func eventKindIs(kind string) m.Matcher {
	return m.JSONProperty("kind").Should(m.Equal(kind))
}

func anyIndexEvent() m.Matcher {
	return eventKindIs("index")
}

func anyIdentifyEvent() m.Matcher {
	return eventKindIs("identify")
}

func anyFeatureEvent() m.Matcher {
	return eventKindIs("feature")
}

func anyCustomEvent() m.Matcher {
	return eventKindIs("custom")
}

func anySummaryEvent() m.Matcher {
	return eventKindIs("summary")
}

func identifyEventForContextKey(key string) m.Matcher {
	return m.AllOf(
		eventKindIs("identify"),
		m.JSONProperty("context").Should(m.JSONProperty("key").Should(m.Equal(key))),
	)
}

func indexEventForContextKey(key string) m.Matcher {
	return m.AllOf(
		eventKindIs("index"),
		m.JSONProperty("context").Should(m.JSONProperty("key").Should(m.Equal(key))),
	)
}

func featureEventForFlag(flag FlagEventProperties) m.Matcher {
	return m.AllOf(
		m.JSONProperty("kind").Should(m.Equal("feature")),
		m.JSONProperty("key").Should(m.Equal(flag.GetKey())))
}

func featureEventWithAllProperties(sourceEvent FeatureRequestEvent, flag FlagEventProperties) m.Matcher {
	return matchFeatureOrDebugEvent(sourceEvent, flag, false, nil)
}

func debugEventWithAllProperties(sourceEvent FeatureRequestEvent, flag FlagEventProperties, contextJSON interface{}) m.Matcher {
	return matchFeatureOrDebugEvent(sourceEvent, flag, true, contextJSON)
}

func matchFeatureOrDebugEvent(sourceEvent FeatureRequestEvent, flag FlagEventProperties,
	debug bool, inlineContext interface{}) m.Matcher {
	props := map[string]interface{}{
		"kind":         "feature",
		"key":          flag.GetKey(),
		"creationDate": sourceEvent.GetBase().CreationDate,
		"version":      flag.GetVersion(),
		"value":        sourceEvent.Value,
		"default":      nil,
	}
	if debug {
		props["kind"] = "debug"
	}
	if sourceEvent.Variation.IsDefined() {
		props["variation"] = sourceEvent.Variation.IntValue()
	}
	if sourceEvent.Reason.GetKind() != "" {
		props["reason"] = json.RawMessage(jsonhelpers.ToJSON(sourceEvent.Reason))
	}
	if inlineContext == nil {
		props["contextKeys"] = expectedContextKeys(sourceEvent.Context.context)
	} else {
		props["context"] = inlineContext
	}
	return m.JSONEqual(props)
}

func customEventWithEventKey(eventKey string) m.Matcher {
	return m.AllOf(
		eventKindIs("custom"),
		m.JSONProperty("key").Should(m.Equal(eventKey)),
	)
}

func summaryEventWithFlag(flag flagEventPropertiesImpl, counterProps ...[]m.Matcher) m.Matcher {
	counters := make([]m.Matcher, 0, len(counterProps))
	for _, cp := range counterProps {
		counters = append(counters, m.AllOf(
			append(cp, m.JSONProperty("version").Should(m.Equal(flag.GetVersion())))...,
		))
	}
	return m.AllOf(
		m.JSONProperty("kind").Should(m.Equal("summary")),
		m.JSONProperty("features").Should(
			m.JSONProperty(flag.GetKey()).Should(
				m.JSONProperty("counters").Should(m.ItemsInAnyOrder(counters...)),
			),
		),
	)
}

func summaryCounterProps(variation ldvalue.OptionalInt, value ldvalue.Value, count int) []m.Matcher {
	return []m.Matcher{
		m.JSONProperty("value").Should(m.JSONEqual(value)),
		m.JSONProperty("count").Should(m.Equal(count)),
		m.JSONOptProperty("variation").Should(m.JSONEqual(variation)),
	}
}

func summaryCounterPropsFromEval(evalDetail ldreason.EvaluationDetail, count int) []m.Matcher {
	return summaryCounterProps(evalDetail.VariationIndex, evalDetail.Value, count)
}

func valueIsPositiveNonZeroInteger() m.Matcher {
	return m.New(
		func(value interface{}) bool {
			v := ldvalue.Parse(jsonhelpers.ToJSON(value))
			return v.IsInt() && v.IntValue() > 0
		},
		func() string {
			return "is an int > 0"
		},
		func(value interface{}) string {
			return "was not an int or was negative"
		},
	)
}

func expectedContextKeys(c ldcontext.Context) map[string]string {
	if c.Multiple() {
		ret := make(map[string]string)
		for i := 0; i < c.MultiKindCount(); i++ {
			if mc, ok := c.MultiKindByIndex(i); ok {
				ret[string(mc.Kind())] = mc.Key()
			}
		}
		return ret
	}
	return map[string]string{
		string(c.Kind()): c.Key(),
	}
}
