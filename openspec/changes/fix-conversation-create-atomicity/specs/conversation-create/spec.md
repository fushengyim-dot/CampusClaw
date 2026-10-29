## ADDED Requirements

### Requirement: Roll back a failed conversation insert
The service SHALL create a conversation in an explicit transaction. If a statement fails before commit, the transaction SHALL be rolled back, so that releasing a database lock later does not persist the failed insert. It SHALL preserve existing user/class/agent checks and SHALL NOT automatically retry creation.

#### Scenario: Insert times out while waiting for an agent lock
- **WHEN** a conversation insert times out before commit and its transaction is closed
- **THEN** releasing the lock does not create a conversation for that failed request

#### Scenario: Valid creation
- **WHEN** the selected agent is enabled in the authenticated user class and the transaction commits
- **THEN** the returned ID identifies one conversation owned by that user

#### Scenario: Commit acknowledgement fails
- **WHEN** the caller cannot confirm commit success
- **THEN** the page refreshes the owned list and preserves the uncertain result instead of claiming no row was saved
