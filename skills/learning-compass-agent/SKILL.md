---
name: learning-compass-agent
description: Use the token-protected Learning Compass Agent API to inspect or update one family's learning records, plans, and practice evidence.
---

# Learning Compass Agent

Use this skill when the task concerns Learning Compass data rather than a
learner-facing browser workflow.

## Start with discovery

The agent endpoint is `/api/agent/v1`. Send `Authorization: Bearer <token>` on
every request. First call `GET /health`, then `GET /capabilities`; do not assume
an operation exists only because it exists in another deployment.

## Preserve the meaning of learning evidence

Read the current record before replacing it. In particular, Friday spelling
mistakes represent a parent's paper-test observation and should only change when
the parent has confirmed a new list. A review attempt does not automatically
clear those mistakes.

Use narrow writes: update a specific week, topic, book, or assignment rather
than erasing a collection. Ask for direction before clear/delete operations or
before generating AI-backed content that may incur cost.

## Useful routes

- English weekly words and Friday mistakes: `/english/weeks`
- Chinese review queue: `/chinese/review/due`
- Main maths lesson: `/learning-plan/main-lesson`
- Reading books and sessions: `/reading/books`, `/reading/sessions`
- Piano teacher assignments and practice evidence: `/piano/weeks`, `/piano/today`

For Piano, original teacher photos and PDFs are the visual source of truth. OCR
may only suggest text fields for parent confirmation; never infer, create, or
evaluate staff notation.

Read [`../../docs/automation-api.md`](../../docs/automation-api.md) for setup,
error handling, and the TypeScript SDK. Read
[`../../docs/openapi/agent-api.yaml`](../../docs/openapi/agent-api.yaml) when a
request body or route is unclear.
