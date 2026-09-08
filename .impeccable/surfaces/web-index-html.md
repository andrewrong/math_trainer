---
version: 1
slug: "web-index-html"
primary_target: "web/index.html"
related_targets: []
---

## Scope and mode

Home/Today, the Parent weekly-focus control, the primary navigation shell, and the learner's Chinese Tone Focus and Write from Memory modules. Visitor mode: Operate.

## Audience, job, and task

The learner is a Primary 4 child working independently at home. On arrival she should understand the useful next task, start it, return to resume it, and see what follows without searching through subject tools. The parent can pin weekly focus; the system fills the remaining session from curriculum and learning evidence.

In Chinese, she should be able to distinguish the visual contours of tones 2, 3, and 4, then practise retrieving a character on paper from sound and meaning clues before seeing the answer. Both modules support focused independent practice rather than passive recognition.

For writing recall, the parent prepares the dictation from the learner's Weekly Words evidence: choose a week or mixed review, adjust individual characters, then hand over a clean retrieval session with no answers left visible.

## Content and constraints

Default session: Main Lesson (10 min), Fluency Practice (8 min), Needs Review (7 min). Quick 10 uses the highest-priority lesson or review plus fluency. The parent can choose any P4 curriculum topic or enable Auto recommend; the choice persists in SQLite and Today reads it through the learning-plan API. Learner-facing copy is English. Existing practice pages, routes, data, and OCR plans stay intact. Unfinished work persists locally.

Tone Focus is an 18-question session: six questions each for the rising second tone, dipping third tone, and falling fourth tone. It teaches the mark as a visible contour before testing the learner, keeps instructions and evidence-specific feedback in English, and returns missed tone items to later review.

Write from Memory is a configurable paper-writing session sourced from Weekly Words. Eligible dated weeks are ordered newest first and the latest week is the default source. Every Weekly Words card provides an English-first “Dictate This Week · 听写这周” shortcut that opens the writing picker with that week's eligible characters selected. The parent can check or uncheck characters individually, select or clear the whole source, or use “Needs practice” to select only characters with recorded writing errors. “All learned words” is a deduplicated aggregate source with a suggested 10-character selection, prioritising writing errors, while still allowing manual changes. Starting dictation hides the picker and its answer list; it returns only after the session completes.

During dictation, the target character stays hidden while the learner sees pinyin, a meaning clue, and an optional word clue with the target blanked out. Each prompt attempts one automatic local pronunciation playback from `/audio/chinese-syllables/<key>.mp3`. A prominent authored-SVG “Play again” button supports deliberate repetition and exposes clear Playing, Played, and Audio unavailable states; pinyin remains visible as the fallback when audio is absent. After writing on paper, she reveals the character in a 150px rice grid (米字格), compares structure and strokes, and self-checks with “Needs another try” or “Looks right.” The resulting evidence prioritises missed characters in later writing sessions. Learner instructions remain at least 17px and the flow does not introduce scores-as-rewards, streaks, or other gamification.

## Chosen direction

25-Minute Learning Timeline: a calm horizontal three-step path under a compact navigation shell. Only the current step carries the dominant blue action. Weekly progress closes the first viewport.

Tone Focus uses an English-first three-panel contour guide followed by a single-question-at-a-time drill. The rising, dipping, and falling marks are paired with arrows and short movement language so colour is not the only differentiator.

Write from Memory extends the English-first Calm Study Desk in Operate mode with a quiet three-step method—listen and read the clue, write on paper, reveal and compare—followed by a source-and-character picker, then a centred paper-like writing stage. The picker keeps source selection, aggregate review, error-driven review, selection count, and per-character checkboxes legible as one preparation task. The hidden-target sequence protects retrieval practice; the pronunciation control is a calm study aid rather than a game mechanic, the 米字格 reveal remains the focal teaching object, and the two self-check actions record evidence without framing the result as a game.

On desktop and mobile, keep the pronunciation action prominent without displacing the clue, writing instruction, or reveal sequence. On mobile, the writing setup and session collapse to one readable vertical flow. Source controls, pronunciation states, and selection utilities provide accessible button states and at least 44px touch targets; self-check actions stack rather than squeeze side by side, and the active Chinese module tab scrolls into the horizontal tab bar's centre so the learner does not lose module context.

## Memorable moment

The learner can read the whole session as one simple journey: now, next, review.

For tone practice, the three accent marks read as distinct paths at a glance. For writing recall, the answer appears only after the learner commits to paper, then lands in a familiar 米字格 for a calm structure check.

## Unresolved

Today completion is local until the shared LearningEvent and ReviewQueue model is implemented. Auto recommend currently uses curriculum status order; richer scoring should follow the shared mastery model.
