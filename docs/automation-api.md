# Learning Compass Agent API

The Agent API gives a trusted AI control plane access to the same learning
records used by the local Learning Compass application. It is deliberately a
small authenticated seam over the existing learning modules: no duplicate
tables, no second definition of progress, and no browser changes.

## Enable it

Set one long random token outside version control, then restart the server:

```bash
export MATHTRAINER_AUTOMATION_API_TOKEN="$(openssl rand -hex 32)"
mise run dev
```

The API is at `http://macmini:8080/api/agent/v1`. If the token is absent, every
Agent API request returns `503`; the local browser API at `/api/v1` continues to
work unchanged.

Every Agent API request needs:

```http
Authorization: Bearer <token>
Accept: application/json
```

Example discovery request:

```bash
curl http://macmini:8080/api/agent/v1/capabilities \
  -H "Authorization: Bearer $MATHTRAINER_AUTOMATION_API_TOKEN"
```

Use `GET /capabilities` first. It returns the available modules and operations
for this server version, including math foundations, English, Chinese context
stories, curriculum, reading, piano, and speech. Piano source files are evidence only: do not ask an agent to
infer or generate staff notation from them.

## Interface design

`/api/agent/v1` mirrors the stable learning-record operations of `/api/v1` with
the same request and response shapes. The prefix is intentionally separate:
the browser can evolve independently, while a control plane gets one explicit,
token-protected integration contract.

The external interface has three discovery endpoints:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/health` | Verify token acceptance and inspect configured AI providers. |
| `GET` | `/capabilities` | Discover grouped operations before choosing a route. |
| Any advertised method | Capability route | Read or change one learning record. |

The runtime capability index is the complete operation catalogue for this
server. [`openapi/agent-api.yaml`](openapi/agent-api.yaml) gives structured
schemas and examples for the most common integration workflows. The SDK's
generic `request()` method can call every capability route, even where a
convenience helper has not been added yet. For complete bodies, use the response
from the relevant `GET` operation as the source of truth before a `PUT` or
`PATCH`.

## Current high-value workflows

- `POST /english/weeks/{weekId}/story/regenerate` replaces only a malformed or
  stale English context story; its word cards and spelling evidence remain intact.
- `GET` / `POST /chinese/context-stories/{setKey}` reads or generates a short,
  checked Chinese cloze story. A story uses at most eight current weekly words;
  it is rejected and rewritten if it fails mechanical or child-reading checks.
- Friday paper spelling uses `{ "mistakes": ["word"] }`, not `words`. This is
  parent-confirmed evidence and must not be changed from automatic practice.

## Safety and error behaviour

- `401`: missing or incorrect bearer token.
- `503`: the Agent API has not been enabled with a token.
- `400`: invalid learning record or request body.
- `404`: unknown route or missing record.
- `500` / `503`: an internal dependency such as a configured AI or OCR provider
  was unavailable; retry only after checking the error message.

The token is appropriate for a single trusted household control plane on a LAN.
Do not expose this server directly to the public internet, share the token in
prompts, or place it in a repository. Rotating it means changing the environment
variable and restarting the server.

## TypeScript SDK

The dependency-free client is in [`sdk/typescript`](../sdk/typescript). It
provides typed helpers for frequent workflows and a generic `request()` method
for every operation advertised by the capability index.

```ts
import { LearningCompassClient, chineseSetKeyFromLabel } from "@learning-compass/agent-sdk";

const compass = new LearningCompassClient({
  baseUrl: "http://macmini:8080",
  token: process.env.LEARNING_COMPASS_API_TOKEN!,
});

const { capabilities } = await compass.capabilities();
const weeks = await compass.english.weeks();
await compass.english.setFridayMistakes("2026-W38", ["because", "beautiful"]);

const story = await compass.chinese.regenerateContextStory(chineseSetKeyFromLabel("2026-09-24"));
```

The SDK does not hide destructive actions behind broad convenience methods:
callers can see the exact route and payload when they use `put`, `patch`, or
`delete`.
