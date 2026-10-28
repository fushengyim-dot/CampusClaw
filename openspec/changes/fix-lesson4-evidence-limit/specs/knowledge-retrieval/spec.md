# Knowledge retrieval

## ADDED Requirements

### Requirement: Bound evidence sent to chat per request
The system SHALL let the caller choose a maximum of 1 to 4 retrieved evidence chunks for each `/api/ask` request. When the field is omitted, the system SHALL use a maximum of 1. The server SHALL reject values outside the range before calling the chat service. The server SHALL include no more than the selected maximum in the model messages, SHALL return citations for only those included chunks, and SHALL report the actual included count and selected maximum in the response.

#### Scenario: Default limit is applied when the field is absent
- **WHEN** an authenticated caller submits an evidence-backed question without `max_evidence_chunks`
- **THEN** at most one retrieved chunk is included in the chat request
- **AND** the response reports `max_evidence_chunks` as 1 and `evidence_count` equal to the citations count

#### Scenario: Explicit limit caps the chat evidence
- **WHEN** an authenticated caller submits a question with `max_evidence_chunks` set to 2 and retrieval returns four chunks
- **THEN** the chat request contains only the first two retrieved chunks
- **AND** the response citations contain only those two chunks
- **AND** the response reports an evidence count of 2 and a maximum of 2

#### Scenario: Invalid limit is rejected before chat
- **WHEN** the requested maximum is not an integer in the inclusive range 1 through 4
- **THEN** the system returns a validation error
- **AND** the chat service is not called

#### Scenario: No evidence still skips chat
- **WHEN** retrieval returns no eligible chunks
- **THEN** the system returns the fixed no-evidence answer and an empty citations list
- **AND** the response evidence count is 0
- **AND** the chat service is not called
