package ldevents

import "encoding/json"

type nullEventProcessor struct{}

// NewNullEventProcessor creates a no-op implementation of EventProcessor.
func NewNullEventProcessor() EventProcessor {
	return nullEventProcessor{}
}

func (n nullEventProcessor) RecordEvaluation(ed EvaluationData) {}

func (n nullEventProcessor) RecordIdentifyEvent(e IdentifyEventData) {}

func (n nullEventProcessor) RecordCustomEvent(e CustomEventData) {}

func (n nullEventProcessor) RecordRawEvent(data json.RawMessage) {}

func (n nullEventProcessor) Flush() {}

func (n nullEventProcessor) Close() error {
	return nil
}
