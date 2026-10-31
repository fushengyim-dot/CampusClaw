## ADDED Requirements
### Requirement: Authorized bounded capture
The service SHALL capture exact request and response body bytes only for configured authenticated cases, SHALL persist attempts before sending, SHALL reject exhausted budgets and mismatched scope, and SHALL NOT log credentials or retry requests.
#### Scenario: Scope or budget fails
- **WHEN** the request is not an authorized empty conversation or its budget is exhausted
- **THEN** no outbound request is made
#### Scenario: Stream is received
- **WHEN** SSE response bytes arrive
- **THEN** bytes reach the existing parser incrementally and the capture preserves bytes and timing with protocol completion status
#### Scenario: Capture write fails or response ends early
- **WHEN** evidence cannot be written or a stream lacks its terminal marker
- **THEN** the attempt is incomplete and further case calls are blocked
#### Scenario: Capture is disabled
- **WHEN** no capture configuration is selected
- **THEN** ordinary operation is unchanged and no evidence bodies are recorded

#### Scenario: Material fingerprint is configured
- **WHEN** capture is enabled
- **THEN** the recorder validates configured material identity, chunk identity, Unicode length and SHA-256 without built-in test data

#### Scenario: Upstream rejects a request
- **WHEN** the upstream returns a non-success HTTP status
- **THEN** the bounded error body is recorded and the case cannot issue another request
