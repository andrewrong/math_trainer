# English IPA and Pronunciation: Service Check and Local Plan

Status: recommendation, 2026-09-08
Scope: investigate the existing Dictionary API dependency and choose a reliable pronunciation/IPA path for the local Go application. No application code is changed by this document.

## Conclusion

`api.dictionaryapi.dev` has **not changed its v2 interface**. It is presently unhealthy from the Mac mini: both valid word requests were accepted by Cloudflare but ended in HTTP `522` after about 19 seconds. That is an origin timeout, not a missing word, a browser-audio failure, DNS, TLS, or an incorrect endpoint in this application.

Do not replace one unauthenticated public API with another as the primary dependency. The recommended product architecture is:

1. ship a pinned, local **CMU Pronouncing Dictionary** snapshot for `en-US` lexical pronunciations and IPA;
2. generate/cache audio locally with the operating-system speech voice for this private Mac mini deployment;
3. use a local **eSpeak NG** process only as an explicit fallback for out-of-vocabulary words, labelled “generated pronunciation”; and
4. keep the current network API only as a non-blocking enrichment fallback, never on the critical rendering path.

This makes normal word-card IPA and audio work offline, removes a third-party availability dependency, and keeps a clear distinction between a dictionary pronunciation and a best-effort generated one.

## What was verified

### The interface remains valid

The service’s current official documentation still specifies:

```text
GET https://api.dictionaryapi.dev/api/v2/entries/en/<word>
```

Its documented response places IPA in `phonetic` and/or `phonetics[].text`; the project says v1 remains supported for backward compatibility. [Official API site](https://dictionaryapi.dev/) and [upstream README](https://github.com/meetDeveloper/freeDictionaryAPI#readme).

Therefore the application’s v2 request shape is still correct. There is no evidence of an endpoint migration.

### The hosted backend is currently degraded from this location

On 2026-09-08 (Asia/Kuala_Lumpur), direct IPv4 HTTPS requests from this Mac mini to both `/api/v2/entries/en/hello` and `/api/v2/entries/en/procedure` completed DNS and TLS successfully, then returned Cloudflare HTTP `522` after roughly 19 seconds. `522` means the edge could not obtain a timely response from the origin; it is not a lexical “word not found” response.

This agrees with current upstream reports: issue [#249](https://github.com/meetDeveloper/freeDictionaryAPI/issues/249) reports 522 failures, while [#241](https://github.com/meetDeveloper/freeDictionaryAPI/issues/241) reports intermittent 522/CORS failures. The upstream README itself says rapidly increased API traffic and AWS cost make the service difficult to keep running, and reports more than ten million monthly requests. [Upstream README](https://github.com/meetDeveloper/freeDictionaryAPI#readme).

This is enough to treat the service as **best-effort only**. It may recover without an application change, but it is not a reliable production dependency for a learning screen.

## Candidate resources

| Resource | What it provides | License / constraint | Local deployment fit | Decision |
| --- | --- | --- | --- | --- |
| [CMU Pronouncing Dictionary](https://github.com/cmusphinx/cmudict) | Curated US-English word → ARPAbet pronunciations, including variants. The companion tools provide a maintained ARPAbet-to-IPA phone table for `en-US`. [Format and phone-table docs](https://github.com/cmusphinx/cmudict-tools#cmu-pronunciation-dictionary-file-format) | BSD-style redistribution terms: retain copyright, conditions, and disclaimer in source/binary distributions. [License](https://github.com/cmusphinx/cmudict/blob/master/LICENSE) | Excellent: store a pinned source revision or prebuilt SQLite table in the app; no runtime network or model. | **Primary IPA source.** |
| [macOS AVSpeechSynthesizer](https://developer.apple.com/documentation/avfaudio/avspeechsynthesizer/write%28_%3Atobuffercallback%3A%29) | On-device speech synthesis; Apple exposes audio-buffer output. | System capability, tied to the Mac and installed voice assets; do not treat generated audio as a transferable voice-data set. | Excellent for this Mac-mini-only deployment. Generate on demand, cache local files, and retain the voice identifier/version. | **Primary audio renderer here.** |
| [eSpeak NG](https://github.com/espeak-ng/espeak-ng) | Local English text-to-phoneme and speech synthesis. Its CLI supports `--ipa` and `--stdout`. [CLI reference](https://github.com/espeak-ng/espeak-ng/blob/master/src/espeak-ng.1.ronn) | GPL-3.0. [License](https://github.com/espeak-ng/espeak-ng/blob/master/COPYING) | Technically straightforward as a separate local executable/service. Its generated IPA is rule-based, so it is useful for unknown words but not a substitute for a curated learner-facing dictionary. | **Fallback only; review licence before bundling/distribution.** |
| [English Wiktionary dumps](https://dumps.wikimedia.org/enwiktionary/) + Commons media | Potential IPA, definitions, and some human recordings. | Text is generally CC BY-SA 4.0/GFDL; media is file-specific and must be checked individually. [Wikimedia dump licensing](https://dumps.wikimedia.org/legal.html) | High ingestion, parsing, provenance, attribution, and per-audio-license complexity. Coverage is uneven. | Do **not** use in phase 1. Consider only if human recordings become a product requirement. |
| [Merriam-Webster API](https://dictionaryapi.com/) | Hosted commercial/reference dictionary API. | Key required; official free tier is non-commercial, max 1,000 queries/day/key, limited to two APIs. | Not local and adds another uptime/key constraint. | Not a local replacement. |

## Recommended implementation shape

### 1. Make pronunciation a local data capability

At build/import time, parse a pinned CMUdict revision into a small SQLite table (or a read-only bundled file):

```text
word_key | locale | pronunciation_rank | arpabet | ipa | source | source_revision
```

Use the official `cmudict-tools` `en-US` IPA mapping rather than inventing a conversion table. Persist all lexical variants and pick the first by default; the UI can expose a compact alternate-pronunciation selector later. CMUdict is US English, so label the result `US IPA` rather than implying universal English pronunciation.

The lookup request should return immediately from local storage. It must never wait for a third-party dictionary before rendering a word card.

### 2. Treat audio as a separately cacheable asset

For a word selected in the UI, synthesize local audio on first use with `AVSpeechSynthesizer`, write/cache it under a key such as:

```text
en-US/<voice-id>/<normalised-word>/<voice-version>.m4a
```

Return a local URL and cache metadata. This preserves the current useful behaviour—pressing the speaker always works—without depending on Youdao or a remote recording. If the user changes the chosen voice, generate a new asset rather than overwriting the old one.

For a word absent from CMUdict, eSpeak NG can produce audio and IPA locally. Mark the provenance as `generated`, show it only after a fallback occurs, and allow a parent to override it. Proper names, inflections, and non-US variants are the cases most likely to need this review.

### 3. Preserve provenance and a safe fallback order

Expose separate fields/statuses rather than a single vague failure string:

```text
ipa: { value, source: cmudict | generated | remote | none }
audio: { url, source: macos_tts | espeak | remote | browser_tts }
```

Suggested order:

```text
local CMUdict IPA
  → local eSpeak IPA only for a missing entry

cached local audio
  → macOS synthesis
  → eSpeak synthesis
  → browser speech fallback (not a failure state)
```

The current hosted Dictionary API can remain as a background-only enrichment source for definitions or a missing IPA, with a short timeout, circuit breaker, and no user-visible “unavailable” message. Cache successful remote results together with source and retrieval date; never overwrite a curated local result automatically.

## Acceptance checks before changing the UI

1. Import the app’s existing English word sets, including `procedure`, and measure CMUdict hit rate and exact IPA output.
2. Test learner-relevant variants: regular plurals/past tense, names, multiword terms, and UK-vs-US words.
3. Confirm generated audio is playable from the same local host after a server restart and after offline network simulation.
4. Test a missing word: it should retain a usable speaker button, show either a clearly labelled generated IPA or no IPA, and never display an error that implies the word cannot be spoken.
5. Add source/version metadata and a license-notice page before distributing CMUdict-derived data beyond this private local installation.

## Why this is the best first move

CMUdict is small, stable, legally straightforward for this use, and supplies the thing the screen actually needs: a lexical US-English pronunciation. macOS can generate the audio locally. This deliberately avoids downloading and maintaining a heavy speech model merely to read isolated vocabulary words, while retaining eSpeak NG as an entirely offline escape hatch for coverage gaps.

If a later requirement is **human-recorded UK/US audio for every word**, make that a separate licensed-content project. It is not solved safely by scraping another free endpoint or by importing Wiktionary audio without per-file provenance and licence handling.
