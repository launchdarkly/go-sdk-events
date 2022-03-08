package ldevents

import (
	"gopkg.in/launchdarkly/go-jsonstream.v1/jwriter"
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldcontext"
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldtime"
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
	contextFormatter eventContextFormatter
	config           EventsConfiguration
}

func (ef eventOutputFormatter) makeOutputEvents(events []commonEvent, summary eventSummary) ([]byte, int) {
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

func (ef eventOutputFormatter) writeOutputEvent(w *jwriter.Writer, evt commonEvent) {
	if raw, ok := evt.(rawEvent); ok {
		w.Raw(raw.data)
		return
	}

	obj := w.Object()

	switch evt := evt.(type) {
	case FeatureRequestEvent:
		kind := FeatureRequestEventKind
		if evt.Debug {
			kind = FeatureDebugEventKind
		}
		beginEventFields(&obj, kind, evt.BaseEvent.CreationDate)
		obj.Name("key").String(evt.Key)
		obj.Maybe("version", evt.Version.IsDefined()).Int(evt.Version.IntValue())
		if evt.Debug {
			ef.contextFormatter.WriteContext(obj.Name("context"), &evt.Context)
		} else {
			writeContextKeys(&obj, &evt.Context.context)
		}
		obj.Maybe("variation", evt.Variation.IsDefined()).Int(evt.Variation.IntValue())
		evt.Value.WriteToJSONWriter(obj.Name("value"))
		evt.Default.WriteToJSONWriter(obj.Name("default"))
		obj.Maybe("prereqOf", evt.PrereqOf.IsDefined()).String(evt.PrereqOf.StringValue())
		if evt.Reason.GetKind() != "" {
			evt.Reason.WriteToJSONWriter(obj.Name("reason"))
		}

	case CustomEvent:
		beginEventFields(&obj, CustomEventKind, evt.BaseEvent.CreationDate)
		obj.Name("key").String(evt.Key)
		if !evt.Data.IsNull() {
			evt.Data.WriteToJSONWriter(obj.Name("data"))
		}
		writeContextKeys(&obj, &evt.Context.context)
		obj.Maybe("metricValue", evt.HasMetric).Float64(evt.MetricValue)

	case IdentifyEvent:
		beginEventFields(&obj, IdentifyEventKind, evt.BaseEvent.CreationDate)
		ef.contextFormatter.WriteContext(obj.Name("context"), &evt.Context)

	case indexEvent:
		beginEventFields(&obj, IndexEventKind, evt.BaseEvent.CreationDate)
		ef.contextFormatter.WriteContext(obj.Name("context"), &evt.Context)
	}

	obj.End()
}

func beginEventFields(obj *jwriter.ObjectState, kind string, creationDate ldtime.UnixMillisecondTime) {
	obj.Name("kind").String(kind)
	obj.Name("creationDate").Float64(float64(creationDate))
}

func writeContextKeys(obj *jwriter.ObjectState, c *ldcontext.Context) {
	keysObj := obj.Name("contextKeys").Object()
	if c.Multiple() {
		for i := 0; i < c.MultiKindCount(); i++ {
			if mc, ok := c.MultiKindByIndex(i); ok {
				keysObj.Name(string(mc.Kind())).String(mc.Key())
			}
		}
	} else {
		keysObj.Name(string(c.Kind())).String(c.Key())
	}
	keysObj.End()
}

// Transforms the summary data into the format used for event sending.
func (ef eventOutputFormatter) writeSummaryEvent(w *jwriter.Writer, snapshot eventSummary) {
	obj := w.Object()

	obj.Name("kind").String(SummaryEventKind)
	obj.Name("startDate").Float64(float64(snapshot.startDate))
	obj.Name("endDate").Float64(float64(snapshot.endDate))

	flagsObj := obj.Name("features").Object()

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

		flagObj := flagsObj.Name(flagKey).Object()

		firstValue.flagDefault.WriteToJSONWriter(flagObj.Name("default"))

		countersArr := flagObj.Name("counters").Array()

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
			counterObj.Maybe("variation", anotherKey.variation.IsDefined()).Int(anotherKey.variation.IntValue())
			if anotherKey.version.IsDefined() {
				counterObj.Name("version").Int(anotherKey.version.IntValue())
			} else {
				counterObj.Name("unknown").Bool(true)
			}
			anotherValue.flagValue.WriteToJSONWriter(counterObj.Name("value"))
			counterObj.Name("count").Int(anotherValue.count)
			counterObj.End()
		}

		countersArr.End()
		flagObj.End()
	}
	flagsObj.End()
	obj.End()
}
