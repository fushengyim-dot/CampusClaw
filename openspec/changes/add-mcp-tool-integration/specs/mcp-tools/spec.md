## ADDED Requirements

### Requirement: An authenticated MCP tool service

The system SHALL provide a self-hosted HTTP MCP service that implements JSON-RPC 2.0 `initialize`, `tools/list`, and `tools/call` over one Streamable HTTP endpoint. The service SHALL require a valid Bearer token for every method and SHALL return only the two approved read-only software-course tools.

#### Scenario: Initialize and discover the local tools

- **WHEN** the host sends a supported `initialize` request with the configured Bearer token and then sends `tools/list`
- **THEN** the server returns the negotiated handshake profile and the complete schemas for `lookup_course_term` and `next_step_hint`
- **AND** repeated discovery returns a deterministic list without creating additional tools

#### Scenario: Reject an invalid token or malformed request

- **WHEN** a caller sends a missing or incorrect Bearer token, unsupported method, invalid JSON-RPC envelope, or unsupported protocol version
- **THEN** the service refuses the request with a safe HTTP or JSON-RPC error
- **AND** the response does not reveal the expected token or any credential value

#### Scenario: Call a read-only tool

- **WHEN** the host sends `tools/call` for one of the discovered tools with schema-valid bounded arguments
- **THEN** the server returns a bounded text result in the MCP result content array
- **AND** the call does not execute commands, write files, modify course records, or contact a third-party service

### Requirement: Teacher-managed class-scoped MCP servers

The system SHALL let teachers of a class register, view, update, connect, disconnect, and delete that class's MCP servers. The host SHALL store each server's token encrypted on the server side and SHALL expose only whether a token is configured. Other classes and non-teacher accounts SHALL receive the same not-found response for management resources.

#### Scenario: Register and update a server without returning its token

- **WHEN** a teacher registers or updates a server with a name, URL, and token
- **THEN** the service stores the token encrypted and the response includes `token_configured: true`
- **AND** no API response, front-end state after save, log, or error includes the token text
- **AND** a duplicate server name within the same class is rejected with a readable validation error

#### Scenario: Refuse unsafe server addresses

- **WHEN** a teacher submits a disallowed scheme, loopback/private metadata target, or malformed URL
- **THEN** the server refuses to connect and explains the URL restriction without attempting a request

### Requirement: Atomic discovery snapshots and disconnect behavior

The host SHALL complete the MCP handshake and `tools/list` before marking a server connected. Each successful discovery SHALL atomically replace the prior snapshot. Failed discovery or disconnect SHALL mark the server disconnected, clear its currently usable tool list, and prevent calls from using stale entries.

#### Scenario: Replace a changed discovery list

- **WHEN** a connected server returns a different tool list on a later successful discovery
- **THEN** the stored list exactly matches the latest response and contains no stale entries or duplicates
- **AND** any saved skill reference whose description or input schema changed requires teacher reconfirmation before use

#### Scenario: Disconnect and refuse the old tool

- **WHEN** a teacher disconnects a server
- **THEN** the host clears the active tool snapshot and sets the server disconnected
- **AND** any subsequent attempt to call a skill reference to that server is rejected before network I/O
- **AND** a rejected call tied to an existing server is recorded with a safe reason

### Requirement: Bind only discovered tools to a skill

The system SHALL validate each skill tool reference in the exact `tool_name@server_name` format when saving the skill. The reference SHALL resolve within the same class to a connected server and a tool in its current discovery snapshot. The host SHALL include only approved references from the selected enabled skill in a model request.

#### Scenario: Reject an unknown or disconnected reference

- **WHEN** a teacher saves a malformed reference, unknown tool, unknown server, cross-class server, or tool from a disconnected server
- **THEN** the save is rejected with a readable validation error and no partial binding is stored

#### Scenario: Restrict candidate declarations to the selected skill

- **WHEN** a user prompt routes to one enabled skill with confirmed tool references
- **THEN** the host sends only those current discovered declarations to the model
- **AND** it sends no other tool available on that server or in another skill

### Requirement: Host-validated bounded tool orchestration

The model SHALL only propose structured tool calls. The host SHALL revalidate every proposed tool name and argument object against the current discovered schema and skill binding before sending a request to an MCP server. The host SHALL enforce per-tool, per-turn, per-call-count, and aggregate time limits. The host SHALL return tool results or safe failure messages as tool-role data before requesting the next model turn.

#### Scenario: Execute a valid proposed tool call

- **WHEN** the model stub proposes a current skill-bound tool with schema-valid arguments
- **THEN** the host sends the request with that server's Bearer token
- **AND** it appends the bounded result as an untrusted tool message before the next model turn
- **AND** it records the result status without storing the token or raw arguments

#### Scenario: Reject an unbound or invalid proposed call

- **WHEN** the model proposes a tool outside the active candidate list, malformed arguments, or values that violate the current schema
- **THEN** the host does not send a network request
- **AND** it emits a safe rejected tool result and records the rejection when the request can be attributed to an existing server

#### Scenario: Stop safely at an execution limit

- **WHEN** the model exceeds a configured turn, call-count, per-call timeout, or total time limit
- **THEN** the host performs no further tool call
- **AND** the conversation continues using accumulated information with a clear safe explanation

#### Scenario: Continue after an MCP tool failure

- **WHEN** a server is disconnected, times out, returns an invalid result, or reports a tool error
- **THEN** the host records a safe failure where attributable, sends an error tool message to the model, and continues the conversation without terminating the primary answer path

### Requirement: Treat tool descriptions and results as untrusted data

The host SHALL show full discovered tool descriptions to teachers, keep tool descriptions and results in data fields, and apply the required instruction-like text check to returned text. A tool output SHALL NOT be treated as a system instruction or be allowed to change the approved candidate set.

#### Scenario: Block instruction-like tool output

- **WHEN** a tool result matches an instruction-like unsafe pattern
- **THEN** the host rejects that result as untrusted content and returns a safe error message to the model
- **AND** the check is documented as a risk reduction heuristic, not a complete prompt-injection defense

### Requirement: Auditable calls and ordered streaming events

The system SHALL persist one auditable record for every call attributable to a registered server. A record SHALL identify server and tool snapshots, status, triggering user, class, conversation, turn, time, and a bounded success summary or safe failure reason. It SHALL NOT contain bearer tokens, raw arguments, raw tool results, or conversation history. The user SHALL receive tool-call metadata before the first answer text delta.

#### Scenario: Persist and view an attributable result

- **WHEN** a tool call succeeds, fails, times out, or is refused because a registered server is disconnected
- **THEN** the host stores one record with the appropriate status and safe summary
- **AND** only a teacher in the class or the triggering user may view the corresponding records according to the documented UI/API scope

#### Scenario: Emit tool call details before answer text

- **WHEN** a response uses one or more tools
- **THEN** the initial SSE `meta` event starts the response before tool execution, with an empty initial tool list allowed
- **AND** subsequent `tool` events immediately report running and terminal states before any answer `delta` event
- **AND** the UI displays the tool name and outcome before the streamed answer text

#### Scenario: Do not log an un-attributable unknown server

- **WHEN** a model proposes a server reference that no longer resolves to any registered server
- **THEN** the host reports the refusal in the current response but does not create a misleading server audit row
