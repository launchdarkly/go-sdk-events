package ldevents

import (
	"gopkg.in/launchdarkly/go-jsonstream.v1/jwriter"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldtime"
)

// In this file we create the analytics event JSON data that we send to LaunchDarkly. This does not
// always correspond to the shape of the event objects that are fed into EventProcessor.

// Event types
const (
	FeatureRequestEventKind = "feature"
	FeatureDebugEventKind   = "debug"
	CustomEventKind         = "custom"
	IdentifyEventKind       = "identify"
	IndexEventKind          = "index"
	SummaryEventKind        = "summary"
)

type eventOutputFormatter struct {
	userFilter userFilter
	config     EventsConfiguration
}

func (ef eventOutputFormatter) makeOutputEvents(events []Event, summary eventSummary) ([]byte, int) {
	n := len(events)

	w := jwriter.NewWriter()
	arr := w.Array()

	for _, e := range events {
		ef.writeOutputEvent(&w, e)
	}
	if len(summary.counters) > 0 {
		ef.writeSummaryEvent(&w, summary)
		n++
	}

	if n > 0 {
		arr.End()
		return w.Bytes(), n
	}
	return nil, 0
}

func (ef eventOutputFormatter) writeOutputEvent(w *jwriter.Writer, evt Event) {
	obj := w.Object()

	switch evt := evt.(type) {
	case FeatureRequestEvent:
		kind := FeatureRequestEventKind
		if evt.Debug {
			kind = FeatureDebugEventKind
		}
		beginEventFields(&obj, kind, evt.BaseEvent.CreationDate)
		obj.String("key", evt.Key)
		obj.OptInt("version", evt.Version.IsDefined(), evt.Version.IntValue())
		if ef.config.InlineUsersInEvents || evt.Debug {
			obj.Property("user")
			ef.userFilter.writeUser(w, evt.User)
		} else {
			obj.String("userKey", evt.User.GetKey())
		}
		obj.OptInt("variation", evt.Variation.IsDefined(), evt.Variation.IntValue())
		obj.Property("value")
		evt.Value.WriteToJSONWriter(w)
		obj.Property("default")
		evt.Default.WriteToJSONWriter(w)
		obj.OptString("prereqOf", evt.PrereqOf.IsDefined(), evt.PrereqOf.StringValue())
		if evt.Reason.GetKind() != "" {
			obj.Property("reason")
			evt.Reason.WriteToJSONWriter(w)
		}

	case CustomEvent:
		beginEventFields(&obj, CustomEventKind, evt.BaseEvent.CreationDate)
		obj.String("key", evt.Key)
		if !evt.Data.IsNull() {
			obj.Property("data")
			evt.Data.WriteToJSONWriter(w)
		}
		if ef.config.InlineUsersInEvents {
			obj.Property("user")
			ef.userFilter.writeUser(w, evt.User)
		} else {
			obj.String("userKey", evt.User.GetKey())
		}
		if evt.HasMetric {
			obj.Float64("metricValue", evt.MetricValue)
		}

	case IdentifyEvent:
		beginEventFields(&obj, IdentifyEventKind, evt.BaseEvent.CreationDate)
		obj.String("key", evt.User.GetKey())
		obj.Property("user")
		ef.userFilter.writeUser(w, evt.User)

	case indexEvent:
		beginEventFields(&obj, IndexEventKind, evt.BaseEvent.CreationDate)
		obj.Property("user")
		ef.userFilter.writeUser(w, evt.User)
	}

	obj.End()
}

func beginEventFields(obj *jwriter.ObjectState, kind string, creationDate ldtime.UnixMillisecondTime) {
	obj.String("kind", kind)
	obj.Float64("creationDate", float64(creationDate))
}

// Transforms the summary data into the format used for event sending.
func (ef eventOutputFormatter) writeSummaryEvent(w *jwriter.Writer, snapshot eventSummary) {
	obj := w.Object()

	obj.String("kind", SummaryEventKind)
	obj.Float64("startDate", float64(snapshot.startDate))
	obj.Float64("endDate", float64(snapshot.endDate))

	flagsObj := obj.Object("features")

	// snapshot.counters contains composite keys in any order, part of which is the flag key; we want to group
	// them together by flag key. The following multi-pass logic allows us to do this without using maps. It is
	// based on the corresponding Java SDK logic in EventOutputFormatter.java.

	unprocessedKeys := make([]counterKey, 0, 100)
	// starting with a fixed capacity allows this slice to live on the stack unless it grows past that limit
	for key := range snapshot.counters {
		unprocessedKeys = append(unprocessedKeys, key)
	}

	for i, key := range unprocessedKeys {
		if key.key == "" {
			continue // we've already consumed this one when we saw the same flag key further up
		}
		// Now we've got a flag key that we haven't seen before
		flagKey := key.key
		firstValue := snapshot.counters[key]

		flagObj := flagsObj.Object(flagKey)

		flagObj.Property("default")
		firstValue.flagDefault.WriteToJSONWriter(w)

		countersArr := flagObj.Array("counters")

		// Iterate over the remainder of the list to find any more entries for the same flag key.
		for j := i; j < len(unprocessedKeys); j++ {
			var anotherKey counterKey
			var anotherValue *counterValue
			if j == i {
				anotherKey = key
				anotherValue = firstValue
			} else {
				anotherKey = unprocessedKeys[j]
				if anotherKey.key != flagKey {
					continue
				}
				anotherValue = snapshot.counters[anotherKey]
				unprocessedKeys[j].key = "" // clear this one so we'll skip it in the next pass
			}

			counterObj := countersArr.Object()
			counterObj.OptInt("variation", anotherKey.variation.IsDefined(), anotherKey.variation.IntValue())
			if anotherKey.version.IsDefined() {
				counterObj.Int("version", anotherKey.version.IntValue())
			} else {
				counterObj.Bool("unknown", true)
			}
			counterObj.Property("value")
			anotherValue.flagValue.WriteToJSONWriter(w)
			counterObj.Int("count", anotherValue.count)
			counterObj.End()
		}

		countersArr.End()
		flagObj.End()
	}
	flagsObj.End()
	obj.End()
}
