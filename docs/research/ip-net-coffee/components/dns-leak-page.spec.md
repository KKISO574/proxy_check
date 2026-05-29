# DnsLeakPage Specification

## Overview

- Target file: `frontend/src/main.tsx`
- Screenshot: `docs/design-references/ip-net-coffee/dns-desktop.png`
- Interaction model: click-driven test actions and disclosure accordions.

## DOM Structure

- Header with title and description.
- `.dns-feature-grid`
  - four feature cards.
- `.dns-actions`
  - quick test button
  - deep test button
- `.dns-verdict`
  - verdict title and selected node.
- result table card.
- `.article-grid`
  - four `details.article-fold` accordions.

## Computed Style Targets

- Feature cards: four equal cards, 10px radius, centered icon/text.
- Buttons: centered, `46px` height, 8px radius.
- Verdict: full-width banner, 12px radius, safe/warn/danger state color.
- Result table: compact row spacing, dark card container, horizontal scroll on mobile.

## Responsive Behavior

- Desktop: four feature cards and two-column accordions.
- Mobile: feature cards and accordions stack to one column.

## Data Mapping

- DNS state: `node.meta.dns_leak`.
- Quick test: `POST /api/tasks/{id}/run`.
- Deep test: `POST /api/tasks/{id}/miaospeed/run`.
