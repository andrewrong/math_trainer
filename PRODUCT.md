# Learning Compass

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

- The primary learner is one child currently studying Primary 4. She uses the product at home to understand school topics, practise skills, and review mistakes.
- The parent configures learning content, follows progress, reviews evidence, and confirms uncertain OCR results.
- Multi-learner accounts are a future possibility, not a current requirement.

## Product Purpose

Learning Compass is a local-first family learning system that turns school-aligned goals into a coherent learning loop: curriculum goals, diagnosis, today's work, practice, mistake review, progress, and a concise parent view.

Success means the learner can see what to do next without navigating a collection of disconnected tools, while the parent can understand what is secure, what needs attention, and why the system chose the next activity.

## Positioning

Learning Compass connects multiple subjects to one evidence-driven learning path. Its distinguishing mechanism is not a large collection of exercises; it is the shared loop from school curriculum and observed attempts to mastery, targeted review, and an explainable next step.

## Operating Context

- The product runs as a web application on a family Mac mini and is accessed over the home LAN.
- The learner primarily works in English because that is the language she can comfortably understand in the interface.
- The parent may use bilingual English and Chinese guidance where it improves clarity.
- Learning materials may come from the application's exercises, school curriculum, printed worksheets, and eventually photographed homework.
- The current learning scope includes Mathematics, English, Chinese, and teacher-led Piano home practice, with Primary 4 schoolwork as the immediate focus.

## Capabilities and Constraints

- Existing capabilities include curriculum views, daily practice, diagnostics, arithmetic and topic exercises, mistake review, progress history, English and Chinese learning, and Piano assignment tracking.
- Piano is a teacher-material-led practice companion: it keeps the original photo or PDF, a parent-confirmed weekly plan, brief practice reflections, and lesson feedback. It must never invent, redraw, interpret, or score staff notation.
- These capabilities should converge on a shared learning model and daily workflow rather than remain independent destinations.
- Learning data and photographed schoolwork should remain local by default.
- A future local OCR service will propose recognised questions and answers. Uncertain recognition must be confirmed by the parent before it affects mastery or labels an answer wrong.
- Deterministic checking remains authoritative whenever an exercise has a known answer; a general vision or language model must not silently replace it.
- The initial product is designed for one learner. Multi-user support is explicitly undecided.

## Brand Commitments

- Product name: **Learning Compass**.
- Learner-facing language should be English wherever practical.
- Parent-facing explanations may combine English and Chinese.
- Progress language should be supportive, specific, and evidence-based. It should guide the next action without shaming the learner.

## Evidence on Hand

- The existing application and its working learning modules are the primary product evidence.
- Existing curriculum, practice, diagnostic, mistake, English, Chinese, and Piano tracking data and workflows should be preserved while the information architecture is improved.
- Product and OCR architecture research is available at `docs/research/learning-system-ocr-architecture.md`.
- There are no confirmed public testimonials, commercial claims, benchmark claims, or third-party brand assets; future design work must not invent them.

## Product Principles

1. **One clear next step:** every session should make the learner's next useful action obvious.
2. **Evidence before judgement:** mastery and review decisions must be traceable to real attempts and confirmed inputs.
3. **School-aligned, not content-heavy:** organise learning around current curriculum goals and prerequisite skills rather than the number of available exercises.
4. **Review completes the loop:** mistakes must lead to focused re-practice and a later re-check, not merely accumulate in a list.
5. **Local and parent-legible:** preserve family privacy and explain recommendations in language the parent can act on.

## Accessibility & Inclusion

- Learner-facing copy must use age-appropriate English and avoid unnecessary complexity.
- Interfaces should remain usable on the desktop and tablet-sized browsers used around the home.
- Colour must not be the only signal for correctness, confidence, mastery, or urgency.
