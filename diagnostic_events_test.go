package ldevents

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldtime"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"

	m "github.com/launchdarkly/go-test-helpers/v3/matchers"
)

func TestDiagnosticIDHasRandomID(t *testing.T) {
	id0 := NewDiagnosticID("sdkkey")
	m.In(t).Assert(id0, m.JSONProperty("diagnosticId").Should(m.Not(m.Equal(""))))

	id1 := NewDiagnosticID("sdkkey")
	m.In(t).Assert(id1, m.JSONProperty("diagnosticId").Should(
		m.AllOf(
			m.Not(m.Equal("")),
			m.Not(m.Equal(id0.GetByKey("diagnosticId").StringValue())),
		),
	))
}

func TestDiagnosticIDUsesLast6CharsOfSDKKey(t *testing.T) {
	id := NewDiagnosticID("1234567890")
	m.In(t).Assert(id, m.JSONProperty("sdkKeySuffix").Should(m.Equal("567890")))
}

func TestSetSDKKeyReplacesSuffixAndKeepsDiagnosticID(t *testing.T) {
	id := NewDiagnosticID("old-key-111111")
	dm := NewDiagnosticsManager(id, ldvalue.Null(), ldvalue.Null(), time.Now(), nil)
	dm.SetSDKKey("new-key-222222")

	expectedID := ldvalue.ObjectBuild().
		Set("diagnosticId", id.GetByKey("diagnosticId")).
		SetString("sdkKeySuffix", "222222").
		Build()
	m.In(t).Assert(dm.CreateInitEvent(), m.JSONProperty("id").Should(m.JSONEqual(expectedID)))
	m.In(t).Assert(dm.CreateStatsEventAndReset(0, 0, 0), m.JSONProperty("id").Should(m.JSONEqual(expectedID)))
}

func TestSetSDKKeyUsesWholeKeyWhenShorterThan6Chars(t *testing.T) {
	dm := NewDiagnosticsManager(NewDiagnosticID("sdkkey"), ldvalue.Null(), ldvalue.Null(), time.Now(), nil)
	dm.SetSDKKey("abc")

	m.In(t).Assert(dm.CreateStatsEventAndReset(0, 0, 0),
		m.JSONProperty("id").Should(m.JSONProperty("sdkKeySuffix").Should(m.Equal("abc"))))
}

func TestSetSDKKeyDoesNotChangePendingStreamInits(t *testing.T) {
	dm := NewDiagnosticsManager(NewDiagnosticID("sdkkey"), ldvalue.Null(), ldvalue.Null(), time.Now(), nil)
	dm.RecordStreamInit(10000, true, 100)
	dm.SetSDKKey("new-key-222222")
	dm.RecordStreamInit(20000, false, 50)
	event := dm.CreateStatsEventAndReset(0, 0, 0)

	m.In(t).Assert(event, m.AllOf(
		m.JSONProperty("id").Should(m.JSONProperty("sdkKeySuffix").Should(m.Equal("222222"))),
		m.JSONProperty("streamInits").Should(m.Items(
			m.JSONStrEqual(`{"timestamp": 10000, "failed": true, "durationMillis": 100}`),
			m.JSONStrEqual(`{"timestamp": 20000, "failed": false, "durationMillis": 50}`),
		)),
	))
}

func TestSetSDKKeyIsSafeDuringEventCreation(t *testing.T) {
	dm := NewDiagnosticsManager(NewDiagnosticID("sdkkey"), ldvalue.Null(), ldvalue.Null(), time.Now(), nil)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			dm.SetSDKKey("key-" + strconv.Itoa(i))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = dm.CreateInitEvent()
			_ = dm.CreateStatsEventAndReset(0, 0, 0)
		}
	}()
	wg.Wait()
}

func TestDiagnosticInitEventBaseProperties(t *testing.T) {
	id := NewDiagnosticID("sdkkey")
	startTime := time.Now()
	dm := NewDiagnosticsManager(id, ldvalue.Null(), ldvalue.Null(), startTime, nil)
	event := dm.CreateInitEvent()

	m.In(t).Assert(event, m.AllOf(
		m.JSONProperty("kind").Should(m.Equal("diagnostic-init")),
		m.JSONProperty("id").Should(m.JSONEqual(id)),
		m.JSONProperty("creationDate").Should(equalNumericTime(ldtime.UnixMillisFromTime(startTime))),
	))
}

func TestDiagnosticInitEventConfigData(t *testing.T) {
	id := NewDiagnosticID("sdkkey")
	configData := ldvalue.ObjectBuild().SetString("things", "stuff").Build()
	dm := NewDiagnosticsManager(id, configData, ldvalue.Null(), time.Now(), nil)
	event := dm.CreateInitEvent()

	m.In(t).Assert(event, m.JSONProperty("configuration").Should(m.JSONEqual(configData)))
}

func TestDiagnosticInitEventSDKData(t *testing.T) {
	id := NewDiagnosticID("sdkkey")
	sdkData := ldvalue.ObjectBuild().SetString("name", "my-sdk").Build()
	dm := NewDiagnosticsManager(id, ldvalue.Null(), sdkData, time.Now(), nil)
	event := dm.CreateInitEvent()

	m.In(t).Assert(event, m.JSONProperty("sdk").Should(m.JSONEqual(sdkData)))
}

func TestDiagnosticInitEventPlatformData(t *testing.T) {
	id := NewDiagnosticID("sdkkey")
	dm := NewDiagnosticsManager(id, ldvalue.Null(), ldvalue.Null(), time.Now(), nil)
	event := dm.CreateInitEvent()

	m.In(t).Assert(event, m.JSONProperty("platform").Should(m.JSONProperty("name").Should(m.Equal("Go"))))
}

func TestRecordStreamInit(t *testing.T) {
	id := NewDiagnosticID("sdkkey")
	dm := NewDiagnosticsManager(id, ldvalue.Null(), ldvalue.Null(), time.Now(), nil)
	dm.RecordStreamInit(10000, true, 100)
	dm.RecordStreamInit(20000, false, 50)
	event := dm.CreateStatsEventAndReset(0, 0, 0)

	m.In(t).Assert(event, m.JSONProperty("streamInits").Should(m.Items(
		m.JSONStrEqual(`{"timestamp": 10000, "failed": true, "durationMillis": 100}`),
		m.JSONStrEqual(`{"timestamp": 20000, "failed": false, "durationMillis": 50}`),
	)))
}
