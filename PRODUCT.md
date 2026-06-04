# Product

## Register

product

## Users

Nexflow is used by operators, accounting staff, and admins who manage daily marketplace document flow. They work in a production operations context where the next action must be clear, safe, and fast: check intake readiness, review imported Shopee or marketplace records, send valid documents to SML, inspect failures, and maintain channel settings.

## Product Purpose

Nexflow is an Operations Console for assisted document automation. It turns Shopee, Lazada, TikTok, email, and other intake sources into controlled work queues for review, validation, SML sending, settlement reconciliation, logs, and settings. Success means users can run the primary flow, Shopee and marketplace sales to `ขายสินค้าและบริการ / SI` to SML, without confusing the system with the old BillFlow/Henna demo UI or exposing risky technical internals during daily work.

## Brand Personality

Confident, compact, professional.

Nexflow should feel like a production operations tool: calm enough for repeated daily use, precise enough for accounting work, and distinct enough from BillFlow/Henna that a returning customer can immediately tell this is a new product direction.

## Anti-references

- Do not look like the old BillFlow/Henna interface, especially teal logo remnants, old sidebar rhythm, demo copy, or unchanged table/filter layouts.
- Do not feel like a demo or test instance. Avoid "demo path", "customer demo", "test data" framing in active production UI.
- Do not show technical route internals in daily workflows unless the user is in settings, logs, or an explicit failure/debug context.
- Do not overload pages with explanatory cards, repeated summaries, or technical status blocks that compete with the current work queue.
- Do not use lime as body text on light backgrounds. Lime is for primary actions, selected states, and active accents.
- Do not add decorative gradients, glass effects, oversized hero styling, or marketing-page patterns to authenticated admin surfaces.

## Design Principles

1. Daily work first: every page should answer what needs attention now, what is safe to do next, and where to recover if something fails.
2. Safety before speed: SML send, bulk send, archive, delete, purge, restart, disable connection, and critical settings changes must explain impact before the final action.
3. Compact but readable: density is allowed, but hierarchy, contrast, and grouping must keep each control understandable at a glance.
4. One operations vocabulary: shared filter bars, pagination, date range controls, dropdown primitives, badges, and action placement should behave consistently across related pages.
5. Production language only: copy should use real operational terms, not demo narration or implementation jargon.

## Accessibility & Inclusion

Target WCAG AA for production UI. Body text and action text must meet 4.5:1 contrast. Focus states must remain visible for keyboard users. Motion must respect `prefers-reduced-motion`. Mobile layouts must avoid horizontal page overflow, text overlap, and cramped tap targets for common actions.
