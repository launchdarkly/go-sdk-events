module github.com/launchdarkly/go-sdk-events/v3

go 1.24

require (
	github.com/google/uuid v1.1.1
	github.com/launchdarkly/go-jsonstream/v3 v3.1.1
	github.com/launchdarkly/go-sdk-common/v3 v3.5.0
	github.com/launchdarkly/go-test-helpers/v3 v3.0.1
	github.com/stretchr/testify v1.7.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mailru/easyjson v0.7.6 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// v3.6.1 upgraded to the go-jsonstream/v4 and go-sdk-common/v4 core libraries.
// Those /v4 major bumps are a breaking change for customers (Go semantic import
// versioning), so v3.6.1 is retracted in favor of a v3-only release. See SDK-2496.
retract v3.6.1
