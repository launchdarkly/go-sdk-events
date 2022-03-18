package ldevents

import (
	"encoding/json"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldreason"
	"github.com/launchdarkly/go-sdk-common/v3/ldtime"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
)

// EventContext is a combination of the standard Context struct with additional information that
// may be relevant outside of the standard SDK event generation context.
//
// Specifically, this is because ld-relay uses go-sdk-events to post-process events it has
// received from the PHP SDK. In this scenario the PHP SDK will have already applied the
// private-attribute-redaction logic, so there is no need to do any further transformation
// of the context.
//
// That requirement is specific to ld-relay. In regular usage of the Go SDK, we always just use the
// plain Context constructor.
type EventContext struct {
	context       ldcontext.Context
	preserialized json.RawMessage
}

// Context creates an EventContext that is exactly equivalent to the given Context.
func Context(context ldcontext.Context) EventContext {
	return EventContext{context: context}
}

// PreserializedContext creates an EventContext that contains both a Context and its already-computed
// JSON representation. This representation will be written directly to the output with no further
// processing. The properties of the wrapped Context are not important except for its Kind, Key, and
// FullyQualifiedKey, which are used for context deduplication.
func PreserializedContext(context ldcontext.Context, jsonData json.RawMessage) EventContext {
	return EventContext{context: context, preserialized: jsonData}
}

// Event represents an analytics event generated by the client, which will be passed to
// the EventProcessor.  The event data that the EventProcessor actually sends to LaunchDarkly
// may be slightly different.
type Event interface {
	GetBase() BaseEvent
}

// commonEvent is a less restrictive alternative to Event that does not require a User to satisfy.
type commonEvent interface {
	GetCreationDate() ldtime.UnixMillisecondTime
}

// BaseEvent provides properties common to all events.
type BaseEvent struct {
	CreationDate ldtime.UnixMillisecondTime
	Context      EventContext
}

// FeatureRequestEvent is generated by evaluating a feature flag or one of a flag's prerequisites.
type FeatureRequestEvent struct {
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
	// debug is true if this is a copy of an evaluation event that we have queued to be output as a debug
	// event. This field is not exported because it is never part of the input parameters from the application;
	// we debug events only internally, based on DebugEventsUntilDate.
	debug bool
}

// CustomEvent is generated by calling the client's Track method.
type CustomEvent struct {
	BaseEvent
	Key         string
	Data        ldvalue.Value
	HasMetric   bool
	MetricValue float64
}

// IdentifyEvent is generated by calling the client's Identify method.
type IdentifyEvent struct {
	BaseEvent
}

// indexEvent is generated internally to capture user details from other events. It is an implementation
// detail of DefaultEventProcessor, so it is not exported.
type indexEvent struct {
	BaseEvent
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

// NewUnknownFlagEvent creates an evaluation event for a missing flag.
func (f EventFactory) NewUnknownFlagEvent(
	key string,
	context EventContext,
	defaultVal ldvalue.Value,
	reason ldreason.EvaluationReason,
) FeatureRequestEvent {
	fre := FeatureRequestEvent{
		BaseEvent: BaseEvent{
			CreationDate: f.timeFn(),
			Context:      context,
		},
		Key:     key,
		Value:   defaultVal,
		Default: defaultVal,
	}
	if f.includeReasons {
		fre.Reason = reason
	}
	return fre
}

// NewEvalEvent creates an evaluation event for an existing flag.
//
// The isExperiment parameter, if true, means that a full evaluation event should be generated (regardless
// of whether flagProps.RequireFullEvent is true) and the evaluation reason should be included in the event
// (even if it normally would not have been). In the server-side SDK, that is determined by the IsExperiment
// field returned by the evaluator.
func (f EventFactory) NewEvalEvent(
	flagProps FlagEventProperties,
	context EventContext,
	detail ldreason.EvaluationDetail,
	isExperiment bool,
	defaultVal ldvalue.Value,
	prereqOf string,
) FeatureRequestEvent {
	fre := FeatureRequestEvent{
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
	}
	if f.includeReasons || isExperiment {
		fre.Reason = detail.Reason
	}
	if prereqOf != "" {
		fre.PrereqOf = ldvalue.NewOptionalString(prereqOf)
	}
	return fre
}

// GetBase returns the BaseEvent
func (evt FeatureRequestEvent) GetBase() BaseEvent {
	return evt.BaseEvent
}

// GetCreationDate returns CreationDate
func (evt FeatureRequestEvent) GetCreationDate() ldtime.UnixMillisecondTime {
	return evt.BaseEvent.CreationDate
}

// NewCustomEvent creates a new custom event.
func (f EventFactory) NewCustomEvent(
	key string,
	context EventContext,
	data ldvalue.Value,
	withMetric bool,
	metricValue float64,
) CustomEvent {
	ce := CustomEvent{
		BaseEvent: BaseEvent{
			CreationDate: f.timeFn(),
			Context:      context,
		},
		Key:         key,
		Data:        data,
		HasMetric:   withMetric,
		MetricValue: metricValue,
	}
	return ce
}

// GetBase returns the BaseEvent
func (evt CustomEvent) GetBase() BaseEvent {
	return evt.BaseEvent
}

// GetCreationDate returns CreationDate
func (evt CustomEvent) GetCreationDate() ldtime.UnixMillisecondTime {
	return evt.BaseEvent.CreationDate
}

// NewIdentifyEvent constructs a new identify event, but does not send it. Typically, Identify should be
// used to both create the event and send it to LaunchDarkly.
func (f EventFactory) NewIdentifyEvent(context EventContext) IdentifyEvent {
	return IdentifyEvent{
		BaseEvent: BaseEvent{
			CreationDate: f.timeFn(),
			Context:      context,
		},
	}
}

// GetBase returns the BaseEvent
func (evt IdentifyEvent) GetBase() BaseEvent {
	return evt.BaseEvent
}

// GetCreationDate returns CreationDate
func (evt IdentifyEvent) GetCreationDate() ldtime.UnixMillisecondTime {
	return evt.BaseEvent.CreationDate
}

// GetBase returns the BaseEvent
func (evt indexEvent) GetBase() BaseEvent {
	return evt.BaseEvent
}

// GetCreationDate returns CreationDate
func (evt indexEvent) GetCreationDate() ldtime.UnixMillisecondTime {
	return evt.BaseEvent.CreationDate
}

// GetCreationDate for a rawEvent is meaningless but is required by the commonEvent interface
func (evt rawEvent) GetCreationDate() ldtime.UnixMillisecondTime {
	return 0
}
