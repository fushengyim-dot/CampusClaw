# Capture bounded LLM evidence
## Why
Browser network records cannot prove the backend request body or upstream streamed response.
## What Changes
- Add default-off, scoped outbound transport capture with durable per-case budgets and fail-closed writes.
- Preserve request/response bytes, safe metadata and validated local tool results; never store auth headers.
- Validate two authorized empty conversations and fixed material/skill inputs; prohibit retries.
## Impact
Go services and message handler; no schema or provider changes. Temporary runtime capture mount; local evidence excluded from Git.
