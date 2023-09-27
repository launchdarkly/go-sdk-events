package ldevents

import (
	"encoding/json"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldmigration"
	"github.com/launchdarkly/go-sdk-common/v3/ldreason"
	"github.com/launchdarkly/go-sdk-common/v3/ldtime"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
)

// EventInputContext represents context information that is being used as part of the inputs to an
// event-generating action. It is a combination of the standard Context struct with additional
// information that may be relevant outside of the standard SDK event generation context.
//
// Specifically, this is because ld-relay uses go-sdk-events to post-process events it has
// received from the PHP SDK. In this scenario the PHP SDK will have already applied the
// private-attribute-redaction logic, so there is no need to do any further transformation
// of the context.
//
// That requirement is specific to ld-relay. In regular usage of the Go SDK, we always just use the
// plain Context constructor.
type EventInputContext struct {
	context       ldcontext.Context
	preserialized json.RawMessage
}

// Context creates an EventInputContext that is exactly equivalent to the given Context.
func Context(context ldcontext.Context) EventInputContext {
	return EventInputContext{context: context}
}

// PreserializedContext creates an EventInputContext that contains both a Context and its already-computed
// JSON representation. This representation will be written directly to the output with no further
// processing. The properties of the wrapped Context are not important except for its Kind, Key, and
// FullyQualifiedKey, which are used for context deduplication.
func PreserializedContext(context ldcontext.Context, jsonData json.RawMessage) EventInputContext {
	return EventInputContext{context: context, preserialized: jsonData}
}

// BaseEvent provides properties common to all events.
type BaseEvent struct {
	CreationDate ldtime.UnixMillisecondTime
	Context      EventInputContext
}

// EvaluationData is generated by evaluating a feature flag or one of a flag's prerequisites.
type EvaluationData struct {
	BaseEvent
	// Key is the flag key.
	Key string
	// Variation is the result variation index. It is empty if evaluation failed.
	Variation ldvalue.OptionalInt
	// Value is the result value.
	Value ldvalue.Value
	// Default is the default value that was passed in by the application.
	Default ldvalue.Value
	// Version is the flag version. It is empty if the flag was not found.
	Version ldvalue.OptionalInt
	// PrereqOf is normally empty, but if this evaluation was done for a prerequisite, it is the key of the
	// original key that referenced this flag as a prerequisite.
	PrereqOf ldvalue.OptionalString
	// Reason is the evaluation reason, if the reason should be included in the event, or empty otherwise.
	Reason ldreason.EvaluationReason
	// RequireFullEvent is true if an individual evaluation event should be included in the output event data,
	// or false if this evaluation should only produce summary data (and potentially an index event).
	RequireFullEvent bool
	// DebugEventsUntilDate is non-zero if event debugging has been temporarily enabled for the flag. It is the
	// time at which debugging mode should expire.
	DebugEventsUntilDate ldtime.UnixMillisecondTime
	// SamplingRatio determines the 1 in x chance the evaluation event will be sampled.
	SamplingRatio ldvalue.OptionalInt
	// IndexSamplingRatio determines the 1 in x chance the event will generate an index event.
	IndexSamplingRatio ldvalue.OptionalInt
	// ExcludeFromSummaries determines if the event should be included in summary calculations.
	ExcludeFromSummaries bool
	// debug is true if this is a copy of an evaluation event that we have queued to be output as a debug
	// event. This field is not exported because it is never part of the input parameters from the application;
	// we debug events only internally, based on DebugEventsUntilDate.
	debug bool
}

// CustomEventData is generated by calling the client's Track method.
type CustomEventData struct {
	BaseEvent
	Key         string
	Data        ldvalue.Value
	HasMetric   bool
	MetricValue float64
	// SamplingRatio determines the 1 in x chance the custom event will be sampled.
	SamplingRatio ldvalue.OptionalInt
	// IndexSamplingRatio determines the 1 in x chance the event will generate an index event.
	IndexSamplingRatio ldvalue.OptionalInt
}

// IdentifyEventData is generated by calling the client's Identify method.
type IdentifyEventData struct {
	BaseEvent
	// SamplingRatio determines the 1 in x chance the identify event will be sampled.
	SamplingRatio ldvalue.OptionalInt
}

// MigrationOpEventData is generated through the migration op tracker provided
// through the SDK.
type MigrationOpEventData struct {
	BaseEvent
	Op               ldmigration.Operation
	FlagKey          string
	Version          ldvalue.OptionalInt
	Evaluation       ldreason.EvaluationDetail
	Default          ldmigration.Stage
	SamplingRatio    ldvalue.OptionalInt
	ConsistencyCheck *ldmigration.ConsistencyCheck
	Invoked          map[ldmigration.Origin]struct{}
	Error            map[ldmigration.Origin]struct{}
	Latency          map[ldmigration.Origin]int
}

// indexEvent is generated internally to capture user details from other events. It is an implementation
// detail of DefaultEventProcessor, so it is not exported.
type indexEvent struct {
	BaseEvent
	SamplingRatio ldvalue.OptionalInt
}

// rawEvent is used internally when the Relay Proxy needs to inject a JSON event into the outbox that
// will be sent exactly as is with no processing.
type rawEvent struct {
	data json.RawMessage
}

// FlagEventProperties contains basic information about a feature flag that the events package needs. This allows
// go-sdk-events to be implemented independently of go-server-side-evaluation where the flag model is defined. It
// also allows us to use go-sdk-events in a client-side Go SDK, where the flag model will be different, if we ever
// implement a client-side Go SDK.
type FlagEventProperties struct {
	// Key is the feature flag key.
	Key string
	// Version is the feature flag version.
	Version int
	// RequireFullEvent is true if the flag has been configured to always generate detailed event data.
	RequireFullEvent bool
	// DebugEventsUntilDate is non-zero if event debugging has been temporarily enabled for the flag. It is the
	// time at which debugging mode should expire.
	DebugEventsUntilDate ldtime.UnixMillisecondTime
}

// EventFactory is a configurable factory for event objects.
type EventFactory struct {
	includeReasons bool
	timeFn         func() ldtime.UnixMillisecondTime
}

// NewEventFactory creates an EventFactory.
//
// The includeReasons parameter is true if evaluation events should always include the EvaluationReason (this is
// used by the SDK when one of the "VariationDetail" methods is called). The timeFn parameter is normally nil but
// can be used to instrument the EventFactory with a source of time data other than the standard clock.
//
// The isExperimentFn parameter is necessary to provide the additional experimentation behavior that is
func NewEventFactory(includeReasons bool, timeFn func() ldtime.UnixMillisecondTime) EventFactory {
	if timeFn == nil {
		timeFn = ldtime.UnixMillisNow
	}
	return EventFactory{includeReasons, timeFn}
}

// NewUnknownFlagEvaluationData creates EvaluationData for a missing flag.
func (f EventFactory) NewUnknownFlagEvaluationData(
	key string,
	context EventInputContext,
	defaultVal ldvalue.Value,
	reason ldreason.EvaluationReason,
	indexSamplingRatio ldvalue.OptionalInt,
) EvaluationData {
	ed := EvaluationData{
		BaseEvent: BaseEvent{
			CreationDate: f.timeFn(),
			Context:      context,
		},
		Key:                key,
		Value:              defaultVal,
		Default:            defaultVal,
		IndexSamplingRatio: indexSamplingRatio,
	}
	if f.includeReasons {
		ed.Reason = reason
	}
	return ed
}

// NewEvaluationData creates EvaluationData for an existing flag.
//
// The isExperiment parameter, if true, means that a full evaluation event should be generated (regardless
// of whether flagProps.RequireFullEvent is true) and the evaluation reason should be included in the event
// (even if it normally would not have been). In the server-side SDK, that is determined by the IsExperiment
// field returned by the evaluator.
func (f EventFactory) NewEvaluationData(
	flagProps FlagEventProperties,
	context EventInputContext,
	detail ldreason.EvaluationDetail,
	isExperiment bool,
	defaultVal ldvalue.Value,
	prereqOf string,
	samplingRatio,
	indexSamplingRatio ldvalue.OptionalInt,
	excludeFromSummaries bool,
) EvaluationData {
	ed := EvaluationData{
		BaseEvent: BaseEvent{
			CreationDate: f.timeFn(),
			Context:      context,
		},
		Key:                  flagProps.Key,
		Version:              ldvalue.NewOptionalInt(flagProps.Version),
		Variation:            detail.VariationIndex,
		Value:                detail.Value,
		Default:              defaultVal,
		RequireFullEvent:     isExperiment || flagProps.RequireFullEvent,
		DebugEventsUntilDate: flagProps.DebugEventsUntilDate,
		SamplingRatio:        samplingRatio,
		IndexSamplingRatio:   indexSamplingRatio,
		ExcludeFromSummaries: excludeFromSummaries,
	}
	if f.includeReasons || isExperiment {
		ed.Reason = detail.Reason
	}
	if prereqOf != "" {
		ed.PrereqOf = ldvalue.NewOptionalString(prereqOf)
	}
	return ed
}

// NewCustomEventData creates input parameters for a custom event. No event is actually generated until you
// call EventProcessor.RecordCustomEvent.
func (f EventFactory) NewCustomEventData(
	key string,
	context EventInputContext,
	data ldvalue.Value,
	withMetric bool,
	metricValue float64,
	samplingRatio,
	indexSamplingRatio ldvalue.OptionalInt,
) CustomEventData {
	ce := CustomEventData{
		BaseEvent: BaseEvent{
			CreationDate: f.timeFn(),
			Context:      context,
		},
		Key:                key,
		Data:               data,
		HasMetric:          withMetric,
		MetricValue:        metricValue,
		SamplingRatio:      samplingRatio,
		IndexSamplingRatio: indexSamplingRatio,
	}
	return ce
}

// NewIdentifyEventData constructs input parameters for an identify event. No event is actually generated until you
// call EventProcessor.RecordIdentifyEvent.
func (f EventFactory) NewIdentifyEventData(
	context EventInputContext, samplingRatio ldvalue.OptionalInt,
) IdentifyEventData {
	return IdentifyEventData{
		BaseEvent: BaseEvent{
			CreationDate: f.timeFn(),
			Context:      context,
		},
		SamplingRatio: samplingRatio,
	}
}
