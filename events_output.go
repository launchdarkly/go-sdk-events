package ldevents

import (
	"github.com/launchdarkly/go-jsonstream/v2/jwriter"
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldtime"
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
	if summary.hasCounters() {
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
	case EvaluationData:
		kind := FeatureRequestEventKind
		if evt.debug {
			kind = FeatureDebugEventKind
		}
		beginEventFields(&obj, kind, evt.BaseEvent.CreationDate)
		obj.Name("key").String(evt.Key)
		obj.Maybe("version", evt.Version.IsDefined()).Int(evt.Version.IntValue())
		if evt.debug {
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

	case CustomEventData:
		beginEventFields(&obj, CustomEventKind, evt.BaseEvent.CreationDate)
		obj.Name("key").String(evt.Key)
		if !evt.Data.IsNull() {
			evt.Data.WriteToJSONWriter(obj.Name("data"))
		}
		writeContextKeys(&obj, &evt.Context.context)
		obj.Maybe("metricValue", evt.HasMetric).Float64(evt.MetricValue)

	case IdentifyEventData:
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

	allFlagsObj := obj.Name("features").Object()
	for flagKey, flagSummary := range snapshot.flags {
		flagObj := allFlagsObj.Name(flagKey).Object()

		flagSummary.defaultValue.WriteToJSONWriter(flagObj.Name("default"))

		countersArr := flagObj.Name("counters").Array()
		for counterKey, counterValue := range flagSummary.counters {
			counterObj := countersArr.Object()
			counterObj.Maybe("variation", counterKey.variation.IsDefined()).Int(counterKey.variation.IntValue())
			if counterKey.version.IsDefined() {
				counterObj.Name("version").Int(counterKey.version.IntValue())
			} else {
				counterObj.Name("unknown").Bool(true)
			}
			counterValue.flagValue.WriteToJSONWriter(counterObj.Name("value"))
			counterObj.Name("count").Int(counterValue.count)
			counterObj.End()
		}
		countersArr.End()

		contextKindsArr := flagObj.Name("contextKinds").Array()
		for kind := range flagSummary.contextKinds {
			contextKindsArr.String(string(kind))
		}
		contextKindsArr.End()

		flagObj.End()
	}
	allFlagsObj.End()

	obj.End()
}
