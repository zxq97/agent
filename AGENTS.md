# Repository Guidance

This repository provides Go clients for external services used by the agent. Keep service integrations isolated in `api/` and keep this package transport-independent for its callers.

## Repository Layout

- `api/guide/`: rental-guide service client.
- `api/maps/`: map search and POI-resolution service client.
- Each direct child of `api/` represents one external service package.

## API Package Layout

- `dto.go`: exported request, response, and domain data structures only. All external JSON fields require explicit JSON tags.
- `interface.go`: exported, transport-independent client interface and transport configuration.
- `client.go`: unexported concrete service implementation plus service-specific response envelopes, paths, authentication policy, and DTO parsing.
- `grpc.go`: optional unexported gRPC implementation; add it only for a real gRPC contract.
- `api/*/*_test.go`: remote API integration tests. Define their default `../../conf/dev.yaml` path in the test file and load it through `internal/config.Load`; never use test-only configuration or `httptest` for remote API tests.
- Domain and API tests must use real remote clients configured from `conf/dev.yaml`; do not use fake clients, mock responses, or `httptest` to simulate an external service.

## Design Rules

- Business code depends on exported interfaces such as `guide.Client` and `maps.Client`, never `httpClient` or a raw `net/http.Client`.
- Constructors such as `NewHTTPClient` return the exported interface.
- `pkg/log/` owns context-scoped structured logging. Attach the logger and `trace_id` at the request entrypoint with `log.WithLogger` and `log.WithTraceID`; every emitted record must contain the context trace ID.
- `pkg/http/` owns shared HTTP request construction, bearer-header injection, timeout handling, response-body reads, and all HTTP request logs. It emits exactly one JSON log per request containing `trace_id`, request, response, and `duration_ms`, and returns only `[]byte` response bodies and errors. Service packages must not directly construct `net/http` requests or log HTTP request lifecycle fields.
- `pkg/http/` owns HTTP status-code validation. For any status other than `200 OK`, it must return an error to its caller; API packages must not branch on, handle, or translate non-200 responses.
- Guide sends a Bearer credential only when its contract/config requires one. Maps uses fixed API paths, accepts only an `endpoint` in configuration, and must not send a Bearer credential.
- Pass configuration and request/response DTO structs by pointer at package boundaries. Constructors and methods must handle nil pointers explicitly; scalar values, strings, slices, and byte buffers remain value parameters unless mutation is required.
- When a client applies request defaults, update the supplied DTO pointer directly. Do not make shallow copies solely to avoid mutating a pointer parameter.
- The `pkg/http/` directory declares package `httpclient`, so it can use the standard-library `net/http` package without an import alias. Do not use import aliases unless two imported packages have the same declared name.
- Validate required request and response fields at the API boundary.
- Never wrap an error returned by a lower-level function. Propagate the original error value unchanged with `return err`, including errors from JSON encoding/decoding, parsing, HTTP clients, LLM clients, and external service clients. Do not use `errors.Wrap`, `errors.Wrapf`, `%w`, or formatted copies to add call-site context. Newly constructed errors in the current layer must still start with service and operation, for example `maps resolve: returned incomplete POI`.
- Log request and response summaries through the context-scoped logger, but never secrets such as bearer tokens.
- Provider identifiers, POI IDs, `context_id`, and other server-issued values must originate from API responses; the LLM must not fabricate them.
- Map search returns candidates. Select a candidate only with enough context; do not silently choose the first same-name location across cities.

## Go Workflow

- Format modified Go files with `gofmt`.
- Run `go test ./...` after changes to Go code.
- Avoid import aliases. Use one only to resolve a genuine naming conflict or to follow an unavoidable third-party convention.
- Use `map[T]struct{}` for sets, deduplication, and existence-only lookups. Use `map[T]bool` only when both stored `true` and stored `false` are meaningful values.
- Use `github.com/pkg/errors` consistently when constructing new errors. Prefer `errors.New`; use `errors.Errorf` only when the current layer creates a new formatted error. Returned lower-level errors must be propagated unchanged.
- Keep exported types, methods, and interfaces documented when their meaning is not self-evident.
