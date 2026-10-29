# Design

CreateConversation currently inserts with autocommit. A timed-out insert may complete server-side after the caller has returned. Reproduce using a row lock on a synthetic agent and the configured driver read timeout. Use an explicit transaction with deferred rollback for validation and insertion; commit only after obtaining a valid row ID. A failed COMMIT is still an uncertain result, so keep the existing read-refresh UI and never automatically retry POST. Preserve all existing user/class ownership constraints.
