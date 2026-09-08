---
name: Learning Compass
description: A calm, school-aligned learning workspace that makes the next useful step clear.
colors:
  compass-blue: "#185fa5"
  signal-blue: "#378ADD"
  quiet-sky: "#d6eaf8"
  paper-white: "#ffffff"
  study-ink: "#1a1a1a"
  body-muted: "#666666"
  soft-rule: "#e0e0da"
  input-rule: "#cccccc"
  action-surface: "#e6f1fb"
  support-surface: "#e8f4fc"
  growth-green: "#1D9E75"
  growth-ink: "#146c52"
  growth-surface: "#eaf3de"
  guidance-amber: "#BA7517"
  guidance-surface: "#faeeda"
  correction-red: "#E24B4A"
  correction-ink: "#A32D2D"
  correction-surface: "#fcebeb"
typography:
  display:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif"
    fontSize: "28px"
    fontWeight: 600
    lineHeight: 1.2
  headline:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif"
    fontSize: "24px"
    fontWeight: 900
    lineHeight: 1.35
  title:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif"
    fontSize: "20px"
    fontWeight: 600
    lineHeight: 1.35
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif"
    fontSize: "18px"
    fontWeight: 400
    lineHeight: 1.6
  label:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif"
    fontSize: "14px"
    fontWeight: 800
    lineHeight: 1.35
rounded:
  control: "8px"
  item: "10px"
  card: "12px"
  shell: "16px"
  pill: "999px"
spacing:
  micro: "4px"
  xs: "6px"
  sm: "8px"
  md: "10px"
  lg: "12px"
  card: "16px"
  section: "18px"
components:
  button-primary:
    backgroundColor: "{colors.action-surface}"
    textColor: "{colors.compass-blue}"
    typography: "{typography.body}"
    rounded: "{rounded.control}"
    padding: "13px"
    width: "100%"
  button-secondary:
    backgroundColor: "{colors.support-surface}"
    textColor: "{colors.body-muted}"
    typography: "{typography.label}"
    rounded: "{rounded.control}"
    padding: "7px 14px"
  navigation-active:
    backgroundColor: "{colors.compass-blue}"
    textColor: "{colors.paper-white}"
    typography: "{typography.label}"
    rounded: "{rounded.item}"
    padding: "9px 12px"
    height: "46px"
  card:
    backgroundColor: "{colors.paper-white}"
    textColor: "{colors.study-ink}"
    rounded: "{rounded.card}"
    padding: "16px"
  input:
    backgroundColor: "{colors.paper-white}"
    textColor: "{colors.study-ink}"
    rounded: "{rounded.control}"
    padding: "9px 11px"
  status-chip:
    backgroundColor: "{colors.action-surface}"
    textColor: "{colors.compass-blue}"
    typography: "{typography.label}"
    rounded: "{rounded.pill}"
    padding: "4px 9px"
---

# Design System: Learning Compass

## Overview

**Creative North Star: "The Calm Study Desk"**

Learning Compass should feel like a well-prepared study desk at home: every tool has a clear place, the page is quiet enough to concentrate, and the next useful action is easy to find. The interface is calm, clear, encouraging, and structured. It supports a child working at arm's length on a desktop or tablet without adopting an infantile or entertainment-first tone.

The visual system uses a soft sky field to hold white study surfaces, deep blue to signal direction, and semantic green, amber, and red only when learning evidence calls for them. Density is comfortable rather than sparse: information is grouped into compact cards, but generous type sizes and consistent gaps keep the work legible.

Avoid flashy gamification, neon palettes, excessive cartoon decoration, and the compressed feel of an administration dashboard.

**Key Characteristics:**

- Soft sky background with clean paper-like working surfaces.
- Strong, rare blue states that make the active path unmistakable.
- Large system typography designed for reading at arm's length.
- Thin structural borders, restrained ambient shadows, and predictable rounded geometry.
- Semantic feedback that is supportive and never relies on colour alone.

## Colors

The palette is cool, quiet, and instructional, with warm semantic colours reserved for learning feedback.

### Primary

- **Compass Blue** (`#185fa5`): Marks the active subject, selected navigation, important labels, and the clearest next action.
- **Clear Signal Blue** (`#378ADD`): Carries progress fills, focus borders, interactive hover cues, and explanatory diagrams.

### Secondary

- **Growth Green** (`#1D9E75`): Signals correct work and positive progress; its pale companion is used for mastered or successful surfaces.
- **Growth Ink** (`#146c52`): Accessible positive-status text on white or pale green surfaces.
- **Warm Guidance** (`#BA7517`): Signals hints, uncertainty, and items that need attention without framing them as failure.
- **Gentle Correction** (`#E24B4A`): Marks incorrect answers and targeted re-practice; use it with words or symbols, never as a colour-only judgement.
- **Correction Ink** (`#A32D2D`): Accessible error-status text on white or pale red surfaces.

### Neutral

- **Quiet Sky** (`#d6eaf8`): The global canvas that separates the application from its working surfaces.
- **Paper White** (`#ffffff`): Cards, fields, choices, and content panels.
- **Study Ink** (`#1a1a1a`): Primary reading text and high-emphasis numerals.
- **Body Muted** (`#666666`): Supporting explanations and secondary labels.
- **Soft Rule** (`#e0e0da`): Fine card and section boundaries.
- **Input Rule** (`#cccccc`): Resting outlines on controls and answer fields.
- **Action Surface** (`#e6f1fb`): Pale blue background for primary learning actions and selected chips.
- **Support Surface** (`#e8f4fc`): Low-emphasis instructional blocks and secondary actions.

### Named Rules

**The Compass Blue Is Direction Rule.** Use deep blue for the active path and the most important action, not as decoration across every card.

**The Evidence Colour Rule.** Green, amber, and red must describe a real learning state and must always be paired with text, an icon, or another non-colour cue.

**The Quiet Field Rule.** Keep the sky canvas visible around white working surfaces; do not flood entire screens with saturated colour.

## Typography

**Display Font:** Apple system sans (`-apple-system`, with `BlinkMacSystemFont`, `Segoe UI`, and `sans-serif` fallbacks)
**Body Font:** Apple system sans (`-apple-system`, with `BlinkMacSystemFont`, `Segoe UI`, and `sans-serif` fallbacks)

**Character:** A single familiar system family keeps mixed English and Chinese content stable, fast, and highly legible. Hierarchy comes from size and weight rather than decorative type changes.

### Hierarchy

- **Display** (600, `28px`, 1.2): Product header and rare page-level identity moments.
- **Headline** (900, `24px`, 1.35): Topic-map headings, diagnostic titles, and major learning concepts.
- **Title** (600, `20px`, 1.35): Card and section titles; use weight 900 only when a compact card needs stronger scanning emphasis.
- **Body** (400, `18px`, 1.6): Learner instructions and explanations. Long teaching passages should maintain a comfortable line length and use 1.6–1.9 line-height.
- **Label** (800, `14px`, 1.35): Navigation, status chips, domains, and compact controls. Occasional uppercase labels may use restrained tracking.

### Named Rules

**The Read at Arm's Length Rule.** Learner-facing instructions stay at `17px` or larger; reserve `12–15px` text for metadata, chips, and supporting status only.

**The One Family Rule.** Use the system sans stack across learning surfaces so bilingual text remains coherent; monospace is limited to technical keys or machine-readable values.

## Layout

The application sits in a centred container with a maximum width of `1180px` and `16px` outer padding. The dominant rhythm is an `8–12px` internal gap, `16px` card padding, and `16–18px` separation between major sections. Repeated content uses responsive `auto-fit` grids, usually with minimum card widths between `160px` and `220px`.

Primary subject navigation is a wrapped, centred bar with up to six items per desktop row. At tablet widths it reduces to four items per row; at `640px` and below it becomes a single horizontally scrollable strip with stable `132px` minimum items. Curriculum layouts may use three columns on wide screens, but collapse to a single reading column below `1020px`. Dense visual maps collapse from four or three columns to two at `760px`, then one at `460px`.

**The One Reading Axis Rule.** On narrow screens, preserve a clear vertical learning sequence. Horizontal scrolling is acceptable for navigation and oversized diagrams, not for ordinary lesson content.

**The Comfortable Grid Rule.** Prefer a partially filled final row over stretching cards into unusually wide reading blocks.

## Elevation & Depth

The system is lightly layered. White surfaces and fine borders do most of the structural work; shadows are ambient accents, not the default container treatment. Ordinary cards remain flat. The subject navigation shell, active navigation item, curriculum overview, and hoverable route cards receive limited lift to clarify hierarchy or interactivity.

### Shadow Vocabulary

- **Shell Ambient** (`0 5px 15px rgba(24,95,165,.08)`): The primary navigation container.
- **Active Control** (`0 3px 8px rgba(24,95,165,.2)`): The currently selected subject only.
- **Feature Ambient** (`0 6px 18px rgba(24,95,165,.08)`): A rare overview or learning-map surface.
- **Hover Lift** (`0 5px 12px rgba(24,95,165,.12)`): Hoverable curriculum cards that also move upward by `2px`.

### Named Rules

**The Flat by Default Rule.** Cards and inputs rest without shadows; add elevation only to communicate shell hierarchy, selection, or hover response.

**The Blue Ambient Rule.** Shadows use low-opacity Compass Blue rather than neutral black so depth remains gentle on the sky canvas.

## Shapes

The form language is softly rectangular and predictable. Inputs and compact buttons use `8px` corners, list items and choices use `10px`, cards use `12px`, and major shells or feature maps use `16px`. Pills use a full `999px` radius for short statuses and categories. Circular geometry is reserved for step numbers, progress dots, and icon-like learning markers.

Borders are generally `0.5px` and remain visible enough to establish grouping without becoming a grid of heavy rules. Answer inputs may use a stronger `2px` outline because focus and entry accuracy matter more there.

**The Radius Has Rank Rule.** Larger containers receive larger corners; do not apply pill geometry to cards or rectangular navigation items.

## Components

Components should feel softly tactile, confident, and predictable. State changes are brief and restrained; feedback is immediate without becoming theatrical.

### Buttons

- **Shape:** Gently rounded controls (`8px`) with a fine role-coloured border.
- **Primary:** Full-width pale Action Surface with Compass Blue text, `13px` padding, and a readable `18px` label. It scales to `98%` on active press.
- **Hover / Focus:** Hover may deepen the pale surface or border; focus uses a visible Clear Signal Blue outline or border without removing accessibility cues.
- **Secondary:** Support Surface with muted text and compact `7px 14px` padding. Selected secondary controls may switch to semantic green or Compass Blue styling.
- **Corrective:** Correction Surface and Gentle Correction border/text are reserved for retry or mistake actions.

### Chips

- **Style:** Short labels use full pill corners, compact `4px 9px` padding, and strong `12–14px` text.
- **State:** Blue indicates active learning context, green indicates secure progress, amber indicates review or uncertainty, and red indicates a confirmed error state.

### Cards / Containers

- **Corner Style:** Calm card corners (`12px`), expanding to `16px` for navigation shells and overview maps.
- **Background:** Paper White on Quiet Sky; instructional subregions may use Action Surface or Support Surface.
- **Shadow Strategy:** Flat by default, following the limited vocabulary in Elevation & Depth.
- **Border:** Fine Soft Rule on ordinary cards; contextual borders use the related semantic colour.
- **Internal Padding:** Usually `12–16px`, increasing only for focused quiz or result states.

### Inputs / Fields

- **Style:** Paper White field, `8–10px` corners, Input Rule border, and `9px 11px` padding. Numeric answer fields use a stronger `2px` resting border and larger centred text.
- **Focus:** Shift the border to Clear Signal Blue while retaining a visible focus indication.
- **Error / Disabled:** Error states use Gentle Correction with an explanation; disabled controls reduce opacity but keep their label readable.

### Navigation

Primary navigation lives inside a translucent white shell with `16px` corners. Resting items are transparent and muted, hover states receive a quiet blue surface and `1px` upward movement, and the active item becomes solid Compass Blue with white text. On mobile, navigation becomes a horizontally scrollable row rather than wrapping into a tall menu.

### Learning Feedback

Feedback panels use the semantic surface, matching border, strong heading, and explanatory copy. Correctness must be visible through language and symbols as well as colour. Progress bars use a white track with a thin Soft Rule border and a Clear Signal Blue fill.

### Curriculum Map Card

This signature card combines a short icon, strong concept title, one-line explanation, and a status pill inside a `12px` white tile. Hoverable current topics lift gently; future topics remain flat and visually quieter. The pattern should communicate direction without turning the map into a game board.

## Do's and Don'ts

### Do:

- **Do** keep the Quiet Sky canvas visible around Paper White working surfaces.
- **Do** make one active route or next action unmistakable with Compass Blue.
- **Do** preserve `17–18px` learner-facing text and generous line-height for explanations.
- **Do** use thin borders and restrained spacing to organise dense learning content.
- **Do** pair semantic colours with words, icons, or status labels.
- **Do** collapse multi-column learning layouts into one clear reading axis on narrow screens.

### Don't:

- **Don't** introduce neon colours, glossy gradients, confetti-like decoration, or game-economy visuals.
- **Don't** fill every card or button with Compass Blue; its rarity gives it directional authority.
- **Don't** add heavy black shadows or elevate every surface.
- **Don't** shrink learner instructions to dashboard-sized metadata text.
- **Don't** rely on red or green alone to communicate an answer state.
- **Don't** turn the interface into a dense administration dashboard or an overly childish cartoon world.
