---
name: Nexflow
description: Production Operations Console for marketplace-to-SML document flow
colors:
  background: "#f4f5ef"
  foreground: "#111817"
  card: "#fffefa"
  primary-lime: "#97ff0f"
  primary-foreground: "#111817"
  secondary: "#e8ece4"
  muted: "#eef0ea"
  muted-foreground: "#6a7364"
  cobalt-soft: "#e5ebff"
  cobalt-link: "#1d3491"
  accent-strong: "#38651b"
  destructive: "#dc2828"
  success: "#77c511"
  warning: "#da7707"
  info: "#335cff"
  border: "#d7ddd0"
  sidebar: "#111817"
  sidebar-foreground: "#eaede3"
  sidebar-border: "#293230"
  sidebar-accent: "#252d2a"
typography:
  title:
    fontFamily: "Inter, Noto Sans Thai, system-ui, sans-serif"
    fontSize: "18px"
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: "0"
  body:
    fontFamily: "Inter, Noto Sans Thai, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.55
    letterSpacing: "0"
  label:
    fontFamily: "Inter, Noto Sans Thai, system-ui, sans-serif"
    fontSize: "12px"
    fontWeight: 500
    lineHeight: 1.35
    letterSpacing: "0"
  mono:
    fontFamily: "JetBrains Mono, SFMono-Regular, Consolas, monospace"
    fontSize: "12px"
    fontWeight: 500
    lineHeight: 1.35
rounded:
  sm: "4px"
  md: "6px"
  lg: "8px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "24px"
components:
  button-primary:
    backgroundColor: "{colors.primary-lime}"
    textColor: "{colors.primary-foreground}"
    rounded: "{rounded.md}"
    height: "36px"
    padding: "0 12px"
  button-outline:
    backgroundColor: "{colors.background}"
    textColor: "{colors.foreground}"
    rounded: "{rounded.md}"
    height: "36px"
    padding: "0 12px"
  input:
    backgroundColor: "{colors.background}"
    textColor: "{colors.foreground}"
    rounded: "{rounded.md}"
    height: "40px"
    padding: "8px 12px"
  chip-selected:
    backgroundColor: "{colors.primary-lime}"
    textColor: "{colors.primary-foreground}"
    rounded: "999px"
    height: "28px"
    padding: "0 10px"
---

# Design System: Nexflow

## 1. Overview

**Creative North Star: "The Control Room Ledger"**

Nexflow is a production operations console. The interface should feel like a calm control room for document work: dense enough to scan queues quickly, restrained enough for repeated accounting use, and distinct enough from BillFlow/Henna that users recognize a new product direction immediately.

The system uses a Graphite shell, light operational surfaces, Lime for active work and primary actions, and Cobalt for links or readable action text. It rejects demo presentation patterns, technical route exposition in daily pages, excessive cards, decorative gradients, and UI flourishes that make routine work feel theatrical.

**Key Characteristics:**
- Compact work queues with clear filter and action hierarchy.
- High-contrast action text and status badges.
- Shared primitives for dropdowns, date ranges, pagination, dialogs, and confirmation flows.
- Production copy that names the real workflow: Shopee, `ขายสินค้าและบริการ / SI`, SML, settlement, logs, and settings.

## 2. Colors

The palette is restrained product UI: Graphite for navigation, near-white operational surfaces, Lime as a rare active signal, and Cobalt for readable links.

### Primary
- **Lime Action** (`#97ff0f`): Primary buttons, selected pills, active navigation accents, and progress emphasis. Do not use as normal body text on light backgrounds.
- **Graphite Ink** (`#111817`): Main text, sidebar body, and primary foreground.

### Secondary
- **Cobalt Link** (`#1d3491`): Links and text actions on light backgrounds where Lime would fail readability.
- **Cobalt Soft** (`#e5ebff`): Soft selected or informational backgrounds.

### Tertiary
- **Accent Strong** (`#38651b`): Icon and badge text when the UI needs a green-family accent on light surfaces without Lime glare.

### Neutral
- **Operations Background** (`#f4f5ef`): Main app background with subtle grid texture.
- **Card Surface** (`#fffefa`): Tables, compact headers, dialogs, and form panels.
- **Muted Surface** (`#eef0ea`): Subtle grouped areas and secondary information.
- **Border Neutral** (`#d7ddd0`): Default borders and table dividers.
- **Muted Text** (`#6a7364`): Secondary labels and metadata only.

### Named Rules

**The Lime Is Rare Rule.** Lime is reserved for primary action, selected state, and active navigation. If text must be read on a light surface, use Cobalt Link or Graphite Ink.

**The Graphite Shell Rule.** Navigation stays dark and stable so the work area can remain light, scannable, and low-fatigue.

## 3. Typography

**Display Font:** Inter with Noto Sans Thai fallback.
**Body Font:** Inter with Noto Sans Thai fallback.
**Label/Mono Font:** JetBrains Mono for document numbers, payload identifiers, and technical codes.

**Character:** Typography is compact, neutral, and operational. It should help users compare rows, filters, statuses, and document identifiers without turning product screens into marketing pages.

### Hierarchy
- **Display** (600, 26px max, tight line-height): Rare page-level headers only. Do not use hero-scale type in authenticated screens.
- **Headline** (600, 18-20px): Compact operations headers, document queue titles, and dialog titles.
- **Title** (600, 14-16px): Table row document numbers, card headings, and section labels.
- **Body** (400-500, 14px, 1.55): Normal UI copy, descriptions, and row metadata.
- **Label** (500, 11-12px, no tracking): Filters, badges, status chips, metadata, and compact helper text.
- **Mono** (500, 11-12px): SML doc_no, bill IDs, route codes, and trace-like values.

### Named Rules

**The Product Scale Rule.** Use fixed rem or pixel-based UI type scales. Avoid fluid heading clamps inside product screens.

**The No Eyebrow Rule.** Avoid tiny uppercase tracked labels as repeated section scaffolding. Nexflow should feel operational, not like a generated landing page.

## 4. Elevation

Nexflow uses tonal layering and borders more than shadows. Surfaces should feel placed, not floating. Shadows are acceptable for popovers, dropdowns, dialogs, and transient overlays, but ordinary page sections should rely on border, background, spacing, and hierarchy.

### Shadow Vocabulary
- **Surface Rest** (`box-shadow: none` or existing `shadow-sm` only when needed): Compact headers, tables, and work queue containers.
- **Popover Lift** (`shadow-md`): Dropdowns, select menus, tooltips, and popovers that must escape table or card surfaces.
- **Modal Lift**: Dialog primitives own their overlay and shadow treatment. Do not add decorative outer shadows.

### Named Rules

**The Flat-By-Default Rule.** Stable production surfaces should not float. Use elevation only for overlays and state changes.

## 5. Components

### Buttons
- **Shape:** Rounded medium, 6-8px. Icon buttons may be square with matching radius. Full pill is reserved for status chips.
- **Primary:** Lime background with Graphite text. Use for one clear primary action per workflow area.
- **Hover / Focus:** Use token hover colors and visible `ring` focus. Never remove keyboard focus.
- **Secondary / Ghost:** Use border or muted hover states. Dangerous actions should not sit beside the primary workflow unless a confirmation guard follows.

### Chips
- **Style:** Compact, pill-shaped, 28px height where possible.
- **State:** Selected chips use Lime with Graphite text. Unselected chips use border, background, and muted text.
- **Usage:** Quick status filters stay as chips. Less common filters move to Select or Dropdown primitives.

### Cards / Containers
- **Corner Style:** 8px maximum for page-level operational containers.
- **Background:** Card Surface for primary containers, Muted Surface for quiet grouping.
- **Shadow Strategy:** Border and tonal layering first. Avoid border plus wide soft shadow on the same element.
- **Internal Padding:** Compact by default: 10-16px for operations cards, 24px only for slower settings or onboarding surfaces.

### Inputs / Fields
- **Style:** Border Neutral on Operations Background or Card Surface. Height 32px in dense filter bars, 40px in forms and dialogs.
- **Focus:** Token ring with enough contrast. Keep labels or aria labels for controls.
- **Error / Disabled:** Disabled controls must explain missing prerequisites with tooltip, title, helper text, or surrounding copy when the reason is not obvious.

### Navigation
- **Style:** Graphite sidebar with Lime active item. Desktop uses compact rail or expanded menu. Mobile uses a hamburger drawer with full grouped labels.
- **Typography:** 13-14px labels, no decorative uppercase.
- **States:** Active route is obvious. Hover states must fit the dark shell and user login area.

### Data Tables
- **Style:** Dense rows with clear dividers, readable amounts, status badges, and safe row actions.
- **Behavior:** Filters, search, pagination, row click, and role-gated actions are part of the visual contract. Do not replace them with decorative layouts.

### Date Range Picker
- **Style:** One button with a calendar icon and a popover, matching Logs and Shopee Settlement.
- **Behavior:** It controls `date_from` and `date_to` without changing API semantics.

## 6. Do's and Don'ts

### Do:
- **Do** keep primary marketplace flow visible: Shopee or marketplace intake to `ขายสินค้าและบริการ / SI` to SML.
- **Do** use shared dropdown, date range, pagination, and dialog primitives across related pages.
- **Do** keep dangerous actions in guarded areas with impact and rollback copy.
- **Do** use Cobalt Link for readable text links on light backgrounds.
- **Do** test desktop and mobile for overflow, text overlap, clipped menus, and unreadable badges.
- **Do** preserve existing API payloads, permission behavior, filters, pagination, SML routing, and import confirmation behavior.

### Don't:
- **Don't** make Nexflow look like BillFlow/Henna by reusing teal accents, old logo remnants, demo copy, or unchanged sidebar/table rhythm.
- **Don't** show large Route Inspector or technical routing cards in daily workflows unless the page is settings, logs, or failure diagnostics.
- **Don't** use Lime as small text on light backgrounds.
- **Don't** add new chart, UI, animation, or bitmap dependencies for normal production polish.
- **Don't** use nested cards, card grids as page structure, glassmorphism, gradient text, oversized hero sections, or decorative stripe backgrounds.
- **Don't** hide disabled reasons for production actions such as SML send, bulk send, Shopee disable, purge, restart, or critical settings save.
