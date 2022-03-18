module github.com/launchdarkly/go-sdk-events/v2

go 1.16

require (
	github.com/google/uuid v1.1.1
	github.com/launchdarkly/go-test-helpers/v2 v2.3.1
	github.com/stretchr/testify v1.6.1
	gopkg.in/launchdarkly/go-jsonstream.v1 v1.0.1
	gopkg.in/launchdarkly/go-sdk-common.v3 v3.0.0
)

replace gopkg.in/launchdarkly/go-sdk-common.v3 => github.com/launchdarkly/go-sdk-common-private/v3 v3.0.0-alpha.3
