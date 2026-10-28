# Account registration

## ADDED Requirements

### Requirement: Teacher-issued class-bound invitations
The system SHALL allow an authenticated teacher to issue an invitation for exactly one role (`teacher` or `student`) within the teacher's own class.

#### Scenario: Teacher issues an invitation
- **WHEN** a teacher requests an invitation for an allowed role
- **THEN** the system creates a cryptographically random, single-use invitation bound to that teacher's class, role, issuer, and expiration
- **AND** the plaintext invitation is returned only in the creation response
- **AND** the system stores only a cryptographic digest of the invitation

#### Scenario: Student or cross-class issuer is rejected
- **WHEN** a student attempts to issue an invitation or a teacher addresses another class
- **THEN** the system rejects the request without creating an invitation

### Requirement: Invitation lifecycle
The system SHALL support expiration, teacher revocation, and at-most-once consumption of invitations.

#### Scenario: Teacher revokes an unused invitation
- **WHEN** its issuing teacher revokes an unused invitation in the same class
- **THEN** subsequent registration using it fails

#### Scenario: Concurrent registration attempts use one invitation
- **WHEN** multiple requests concurrently attempt to consume the same invitation
- **THEN** at most one request creates an account and all losing transactions leave no account or partial consumption

### Requirement: Restricted self-registration
The system SHALL create an account only from a valid invitation and SHALL derive its role and class from the invitation.

#### Scenario: Student registration
- **WHEN** a registrant submits a valid student invitation, username, and password
- **AND** the password contains at least 12 Unicode characters and no more than 72 UTF-8 bytes
- **THEN** the created account is a student in the invitation's class
- **AND** the response establishes a normal authenticated session using the existing session protections

#### Scenario: Teacher registration
- **WHEN** a registrant submits a valid teacher invitation, username, and password
- **AND** the password contains at least 12 Unicode characters and no more than 72 UTF-8 bytes
- **THEN** the created account is a teacher in the invitation's class
- **AND** the response establishes a normal authenticated session using the existing session protections

#### Scenario: Client attempts role or class injection
- **WHEN** a registration request includes a role or class field, or attempts to override invitation-bound values
- **THEN** the system rejects the request and does not create an account

#### Scenario: Invalid invitation or duplicate username
- **WHEN** an invitation is unknown, expired, revoked, consumed, or the username is already taken
- **THEN** the system returns a generic registration failure without revealing invitation status or account existence

### Requirement: Preserve existing accounts and login
The system SHALL preserve seeded account identifiers, password hashes, roles, classes, and existing login/session behavior during migration and registration.

#### Scenario: Existing teacher bootstraps invitations
- **WHEN** the existing seeded teacher logs in using the existing credential flow
- **THEN** the teacher can issue invitations for that teacher's own class without any seed credential reset

#### Scenario: Migration preserves users
- **WHEN** the new schema migration is applied or repeated
- **THEN** every existing user and password hash remains unchanged
