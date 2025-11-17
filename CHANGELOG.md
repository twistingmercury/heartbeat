# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Configurable timeout for HTTP dependency checks via `Timeout` field in
  `DependencyDescriptor`
- Request size limiting (1 MB) for HTTP requests to prevent memory exhaustion
  attacks
- TLS/HTTPS support for secure communication with dependencies

### Changed

- Dependency health checks now execute concurrently instead of sequentially,
  significantly improving response times for services with multiple dependencies
- HTTP status code behavior: Returns 200 OK when overall status is OK or Warning,
  503 Service Unavailable when overall status is Critical
- Documentation improvements: Fixed syntax errors in code examples, improved
  clarity and formatting

### Fixed

- Custom handler functions now respect timeout configurations, preventing
  indefinite hangs that could block health check endpoints
- Health check operations now properly support context cancellation, allowing
  graceful handling of client disconnections and request timeouts
- Custom dependency results now consistently populate the Resource field in API
  responses, matching the behavior of HTTP dependencies
- Documentation inaccuracies corrected: default timeout is 10 seconds (not 5),
  HTTP status codes are 200 for OK/Warning and 503 for Critical (not 500),
  removed incorrect claim about 1MB request body limiting

### Security

- HTTP request bodies are now limited to 1 MB to prevent memory exhaustion
  attacks
- Timeout protection prevents hanging connections with configurable timeouts
  (defaults to 10 seconds)
