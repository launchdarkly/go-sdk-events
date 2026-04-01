# Change log

All notable changes to the project will be documented in this file. This project adheres to [Semantic Versioning](http://semver.org).

## [3.6.0](https://github.com/launchdarkly/go-sdk-events/compare/v3.5.1...v3.6.0) (2026-04-01)


### Features

* Add EventMetrics interface for event processing telemetry ([#38](https://github.com/launchdarkly/go-sdk-events/issues/38)) ([9aec7dd](https://github.com/launchdarkly/go-sdk-events/commit/9aec7dd4c4c4a6063384b50573c6b73e3a3fc71c))

## [3.5.1](https://github.com/launchdarkly/go-sdk-events/compare/v3.5.0...v3.5.1) (2026-03-09)


### Bug Fixes

* Bump gopkg.in/yaml.v3 from 3.0.0 to 3.0.1 ([#32](https://github.com/launchdarkly/go-sdk-events/issues/32)) ([623e682](https://github.com/launchdarkly/go-sdk-events/commit/623e682b0a67e9f72d2658925ef7debe8c2b7d43))

## [3.5.0](https://github.com/launchdarkly/go-sdk-events/compare/v3.4.0...v3.5.0) (2025-03-13)


### Features

* Inline context for custom and migration op events ([#28](https://github.com/launchdarkly/go-sdk-events/issues/28)) ([586c241](https://github.com/launchdarkly/go-sdk-events/commit/586c241e1c837816f0e8e0a38596c44ede3bcef5))


### Bug Fixes

* Fix potential deadlock during processor shutdown ([#29](https://github.com/launchdarkly/go-sdk-events/issues/29)) ([e0b7f85](https://github.com/launchdarkly/go-sdk-events/commit/e0b7f859a47b48573b4c812ec30bb0441ddeaecb))

## [3.4.0] - 2024-07-24
### Added:
- Add `EnableCompression` option on `EventSenderConfiguration` to enable gzip compression of event payloads.

### Fixed:
- Add index event when processing preserialized events.

## [3.3.0] - 2024-06-25
### Added:
- Added the ability to optionally omit anonymous contexts from identify and index events.

## [3.2.0] - 2024-03-13
### Changed:
- Redact anonymous attributes within feature events
- Always inline contexts for feature events

## [3.1.0] - 2023-10-23
### Added:
- Add new `ForceSampling` field. This ensures an event is sent regardless of the provided `SamplingRatio`.

## [3.0.0] - 2023-10-11
### Added:
- `EventProcessor` interface now supports recording migration related events.
- Event sampling and summary exclusion controls are now supported.

## [2.0.2] - 2023-05-11
### Fixed:
- Do not queue subsequent events after unrecoverable error in the event processor.
- HTTP status code 413 will no longer trigger an event processor shutdown.

## [2.0.1] - 2023-03-01
### Changed:
- Bumped go-sdk-common to v3.0.1.

## [2.0.0] - 2022-12-01
This major version release of `go-sdk-events` corresponds to the upcoming v6.0.0 release of the LaunchDarkly Go SDK (`go-server-sdk`), and cannot be used with earlier SDK versions. As before, this package is intended for internal use by the Go SDK, and by LaunchDarkly services; other use is unsupported.

### Added:
- `EventProcessor.FlushBlocking`
- `EventProcessor.RecordRawEvent`
- `EventInputContext`
- `NewServerSideEventSender`
- `PreserializedContext`
- `SendEventDataWithRetry`

### Changed:
- The minimum Go version is now 1.18.
- The package now uses a regular import path (`github.com/launchdarkly/go-sdk-events/v2`) rather than a `gopkg.in` path (`gopkg.in/launchdarkly/go-sdk-events.v1`).
- The dependency on `gopkg.in/launchdarkly/go-sdk-common.v2` has been changed to `github.com/launchdarkly/go-sdk-common/v3`.
- Events now use the `ldcontext.Context` type rather than `lduser.User`.
- Private attributes can now be designated with the `ldattr.Ref` type, which allows redaction of either a full attribute or a property within a JSON object value.
- There is a new JSON schema for analytics events. The HTTP headers for event payloads now report the schema version as 4.
- Renamed `FeatureRequestEvent`, `IdentifyEvent`, and `CustomEvent` to `EvaluationData`, `IdentifyEventData`, and `CustomEventData`, to clarify that they are inputs affecting events rather than the events themselves.

### Removed:
- All alias event functionality
- `EventsConfiguration.InlineUsersInEvents`
- `FlagEventProperties`
- `NewDefaultEventSender`

## [1.1.1] - 2021-06-03
### Fixed:
- Updated `go-jsonstream` and `go-sdk-common` dependencies to latest patch versions for JSON parsing fixes. Those patches should not affect `go-sdk-events` since it does not _parse_ JSON, but this ensures that the latest release has the most correct transitive dependencies.

## [1.1.0] - 2021-01-21
### Added:
- Added support for a new analytics event type, &#34;alias&#34;, which will be used in a future version of the SDK.

## [1.0.1] - 2020-12-17
### Changed:
- The library now uses [`go-jsonstream`](https://github.com/launchdarkly/go-jsonstream) for generating JSON output.

## [1.0.0] - 2020-09-18
Initial release of this analytics event support code that will be used with versions 5.0.0 and above of the LaunchDarkly Server-Side SDK for Go.
