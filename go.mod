module github.com/launchdarkly/go-sdk-events/v3

go 1.18

require (
	github.com/google/uuid v1.1.1
	github.com/launchdarkly/go-jsonstream/v3 v3.0.0
	github.com/launchdarkly/go-sdk-common/v3 v3.0.1
	github.com/launchdarkly/go-test-helpers/v3 v3.0.1
	github.com/stretchr/testify v1.7.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mailru/easyjson v0.7.6 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/exp v0.0.0-20220823124025-807a23277127 // indirect
	gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c // indirect
)

replace github.com/launchdarkly/go-sdk-common/v3 => github.com/launchdarkly/go-sdk-common-private/v3 v3.0.0-alpha.6.0.20230829225529-e3a87e3952ac
