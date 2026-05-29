# IpProfilePage Specification

## Overview

- Target file: `frontend/src/main.tsx`
- Screenshot: `docs/design-references/ip-net-coffee/ip-desktop.png`
- Interaction model: static profile display plus click-driven node selection.

## DOM Structure

- Header with title and description.
- `.ip-head.clone-card`
  - main IP identity
  - score pill
  - search input
- `.ip-grid`
  - four `InfoCard` sections.
- full-width ping strip.
- node selector grid.

## Computed Style Targets

- IP head: bordered card, gradient from soft background to card background.
- IP text: about `24px`, weight `900`.
- Score pill: rounded 8px, high/mid/low color state.
- Cards: 10px radius, 1px strong border, dark card background.
- Key/value rows: two columns with dashed separators.

## Responsive Behavior

- Desktop: two-column card grid and three-column node grid.
- Mobile: all cards and node list stack to one column.

## Data Mapping

- Exit IP: `node.meta.exit_ip`.
- Score: `node.score`.
- ASN/ISP/GEO: `node.meta`.
- Network quality: `delay`, `http_rtt`, `tls_handshake`, `packet_loss`.
- Unlock status: `netflix_unlock`, `disney_unlock`, `openai_unlock`, `youtube_unlock`.
