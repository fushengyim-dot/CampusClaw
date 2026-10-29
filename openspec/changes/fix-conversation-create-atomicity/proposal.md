# Make conversation creation transactional

## Why
A create request can report an error while a new conversation is later visible. The current insert uses autocommit. Investigate lock-wait/response timeout behavior using isolated local fixtures, then make the insert atomic.

## What Changes
- Exercise statement timeout during a delayed insert.
- Run creation inside an explicit transaction and roll back failed statements.
- Preserve ownership and agent scope checks.

## Impact
Conversation creation and focused local tests; no schema changes or model requests. Commit acknowledgement loss remains a separately documented uncertainty, not a guarantee of absence.
