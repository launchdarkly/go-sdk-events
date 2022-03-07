package ldevents

import (
	"encoding/json"
	"time"

	"gopkg.in/launchdarkly/go-jsonstream.v1/jwriter"
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldcontext"
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldreason"
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldtime"
	"gopkg.in/launchdarkly/go-sdk-common.v3/ldvalue"
)

const (
	testContextKey = "userKey"

	sdkKey = "SDK_KEY"

	fakeBaseURI       = "https://fake-server"
	fakeEventsURI     = fakeBaseURI + "/bulk"
	fakeDiagnosticURI = fakeBaseURI + "/diagnostic"

	fakeTime = ldtime.UnixMillisecondTime(100000)

	briefRetryDelay = 50 * time.Millisecond
)

var (
	testValue                   = ldvalue.String("value")
	testEvalDetailWithoutReason = ldreason.NewEvaluationDetail(testValue, 2, noReason)
	undefInt                    = ldvalue.OptionalInt{}
	arbitraryJSONData           = []byte(`"hello"`)
)

func fakeTimeFn() ldtime.UnixMillisecondTime { return fakeTime }

func basicContext() EventContext {
	return Context(ldcontext.NewBuilder(testContextKey).Name("Red").Build())
}

func basicConfigWithoutPrivateAttrs() EventsConfiguration {
	return EventsConfiguration{
		Capacity:              1000,
		FlushInterval:         1 * time.Hour,
		UserKeysCapacity:      1000,
		UserKeysFlushInterval: 1 * time.Hour,
	}
}

func contextJSON(c EventContext, config EventsConfiguration) json.RawMessage {
	formatter := newEventContextFormatter(config)
	w := jwriter.NewWriter()
	formatter.WriteContext(&w, &c)
	if err := w.Error(); err != nil {
		panic(err)
	}
	return w.Bytes()
}
