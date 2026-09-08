# Spark Handoff: Singapore Math Curriculum Deepening

## Goal

Finish the P4 Singapore Math curriculum packs so a child can learn a concept, see a visual model, work through examples, practise, self-check with a distinct mini test, and attempt one connected challenge. Preserve all existing SQLite tables, API response shapes, and historic learner data.

The next prerequisite track is P3. `P3-ADD-SUB` and `P3-MULT-DIV` are already deep packs; do not replace them with generic templates.

## Project Context

- Repository: this repository
- Runtime: Go via `mise`; verify with `go version` before commands.
- Main backend content source: `internal/handler/curriculum.go`
- Content depth tests: `internal/handler/curriculum_test.go`
- Curriculum UI: `web/index.html`
- Existing content is original teaching material informed by the Singapore MOE syllabus and My Pals Are Here! 3rd Edition chapter mappings. Do not copy textbook pages or exercises.

## Current State

- P4 packs are generated locally through `buildTopicLessonPack` and persisted as `curriculum_artifacts`.
- Topic UI already supports internal tabs. Keep this structure: `Big Idea`, `Visual Model`, `Worked Examples`, `Practice`, `Mini Test`, `Challenge`, `Parent Notes`, `My Pals`.
- Existing worktree has unrelated uncommitted Chinese, English, UI, and database work. Do not reset, revert, or rewrite unrelated files.
- Last known failure: `TestP4AnglesIncludeNotationAndVertexReasoning` requires the P4 angles pack to teach job selection, evidence, notation, dual protractor scales, compass turns, and shape-property proof explicitly.
- P3 deep packs now exist:
  - `P3-ADD-SUB`: place-value alignment, regrouping as value trade, zero crossing, estimates, and inverse checks. Visual source: `web/static/curriculum/add-sub-regrouping.excalidraw`; web asset: `add-sub-regrouping.svg`.
  - `P3-MULT-DIV`: equal groups, sharing vs grouping, missing multiplication, remainder rule, inverse check, and real-world remainder decisions. Visual source: `division-groups.excalidraw`.
- Versioned artifact refresh is active for `P3-ADD-SUB`, `P3-MULT-DIV`, and all `P4-*` packs. Use `curriculumArtifactVersion` when adding a new deep pack; do not leave it on the generic, non-versioned artifact path.

## Required Content Standard

For each P4 topic, generate a complete lesson pack with:

1. A child-readable big idea: what the concept represents, not a formula list.
2. At least one visual/model spec that the frontend can render.
3. Four or more worked examples with explicit reasoning.
4. Six or more guided-practice questions that teach the method.
5. Four or more different mini-test questions that check transfer without repeating guided questions.
6. One meaningful challenge tied directly to the topic.
7. Common misconceptions that state why the tempting answer is wrong.
8. Brief Chinese parent-coach notes that provide prompts, not long translation lessons.

## Priorities

1. Make P4 topic content internally coherent and visibly useful, especially geometry and spatial reasoning.
2. Keep Practice and Mini Test genuinely distinct.
3. Use diagrams/model metadata that the existing frontend can render; do not add a graphics framework.
4. Add tests when a specific misconception or representation is likely to regress.
5. Do not modify existing database schemas unless a new requirement truly needs it.
6. For P3 work, use this order: `P3-WHOLE-10000`, `P3-FRACTIONS`, `P3-TIME`, `P3-AREA-PERIMETER`, `P3-MEASUREMENT`, `P3-GEOMETRY`, `P3-STATISTICS`.

## P4 Topic Coverage

- Numbers up to 100,000
- Factors and multiples
- Four operations algorithms
- Fractions and mixed/improper fractions
- Decimals and money
- Area and perimeter
- Angles, rectangles, and squares
- Line symmetry
- Nets and 2D representations of solids
- Tables and line graphs

## Execution Loop

1. Inspect the target case in `buildTopicLessonPack` and its matching assertions in `curriculum_test.go`.
2. Improve the actual child-facing content. Never weaken tests merely to make a shallow pack pass.
3. Regenerate P4 artifacts through the existing API only after code/tests pass.
4. Run:

```bash
go test ./internal/handler -count=1
go test ./... -count=1
go build ./cmd/server
node scripts/check_curriculum_ui.js
```

5. Report changed files, which P4 topics changed, test output, and any content that still needs human review.

## One-Topic Rule

Keep Spark tasks small. One task should deepen one topic only:

1. Read the official objectives and My Pals chapter mapping already in the seed data.
2. Create or reuse an accurate visual model. Do not reuse a multiplication diagram for addition/subtraction.
3. Build the lesson pack with child-facing English and short Chinese parent prompts.
4. Add a focused content test and a visual-resource assertion.
5. Bump the applicable artifact version only when the stored content needs refreshing.
6. Run the full validation loop before moving to another topic.

## Guardrails

- Do not copy copyrighted textbook text, page scans, or workbook questions.
- Do not use AI calls during normal page rendering.
- Do not alter old learner progress, math sessions, spelling data, or Chinese data.
- Do not use destructive Git commands.
- Keep every explanation in age-appropriate English; reserve Chinese for `Parent Notes`.
