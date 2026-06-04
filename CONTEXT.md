# Nexflow Context Glossary

## Assisted Automation

Nexflow automates the repeatable parts of document intake, matching, and SML
preparation, but keeps human review at the points where a wrong item, customer,
route, or document number would be costly.

## Operations Console

The admin workspace for daily operational control: readiness, work queues,
imports, SML send history, logs, and settings. `/dashboard` is the first screen;
`/setup` remains visible for production readiness checks.

## Order-to-SML Flow

The path from Email, Shopee, Lazada, or TikTok into a local bill, item mapping,
validation, and final SML document. Production marketplace sales currently route
to `saleinvoice` / `SI`.

## Work Queue

The filtered operational lists for documents that need attention, are ready to
send, failed, sent, archived, or skipped. Filters, pagination, search, and role
permissions are part of the behavior contract.

## SML Send

The controlled send/retry operation that posts a prepared bill to the configured
SML endpoint. It must preserve existing doc_no reuse, validation, channel
overrides, and route selection.

## Preview-First Import

Import flows must preview duplicates, missing SKU mappings, and bill counts
before writing records. QA must not confirm imports unless a controlled smoke
test is explicitly approved.
