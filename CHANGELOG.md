# Change log

All notable changes to the project will be documented in this file. This project adheres to [Semantic Versioning](http://semver.org).

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
