package ldevents

import (
	"time"

	"gopkg.in/launchdarkly/go-sdk-common.v2/ldreason"
	"gopkg.in/launchdarkly/go-sdk-common.v2/lduser"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldvalue"
)

const testUserKey = "userKey"

var testValue = ldvalue.String("value")
var testEvalDetailWithoutReason = ldreason.NewEvaluationDetail(testValue, 2, noReason)

const (
	sdkKey = "SDK_KEY"
)

func basicUser() EventUser {
	return User(lduser.NewUserBuilder(testUserKey).Name("Red").Build())
}

func basicConfigWithoutPrivateAttrs() EventsConfiguration {
	return EventsConfiguration{
		Capacity:              1000,
		FlushInterval:         1 * time.Hour,
		UserKeysCapacity:      1000,
		UserKeysFlushInterval: 1 * time.Hour,
	}
}
