# ip.net.coffee Page Topology

## Shared Shell

1. Top navigation
   - Static horizontal text nav.
   - Active item underlined.
   - Theme toggle aligned to the right.
2. Centered content container
   - `max-width: 1000px`.
   - No left sidebar.
3. Footer
   - Centered, muted text.

## Proxy Check Monitor Page (`/`)

1. Task bar
   - Current task title, source URL, task selector and compact actions.
2. Page header
   - Title and short description.
3. Optional import/edit form and error state.
4. Metric cards
   - Node count, available, down, average delay.
5. Signal board
   - Current node profile summary.
6. Node table
   - Compact table with filters.
7. Detail pane
   - Score, metadata, advanced probes, charts, recent errors.

## IP Profile Page (`/ip/`)

1. Page header
   - `IP 评分查询` title.
2. IP head card
   - Exit IP / selected node identity, country, score, search.
3. Two-column data cards
   - Usage/type.
   - ASN/operator.
   - Technical metrics.
   - Threat/unlock intelligence.
4. Global latency strip
   - Delay, HTTP RTT, TLS, packet loss.
5. Node IP list
   - Selectable node cards.

## DNS Leak Page (`/dns/`)

1. Page header
   - `DNS 泄露检测` title.
2. Feature cards
   - Node DNS, quick test, deep script, result persistence.
3. Test actions
   - Quick test and deep MiaoSpeed test.
4. Verdict banner
   - Safe / warning / danger.
5. DNS result table
   - Node, DNS state, exit IP, country/ASN, last check.
6. Reading accordions
   - Four compact `details` blocks.
