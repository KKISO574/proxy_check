# ip.net.coffee Behavior Notes

## Scope

Reference URLs:

- `https://ip.net.coffee/`
- `https://ip.net.coffee/ip/`
- `https://ip.net.coffee/dns/`

Captured screenshots live in `docs/design-references/ip-net-coffee/`.

## Global Behavior

- Layout is a centered `max-width: 1000px` container with `24px 20px` desktop padding.
- Desktop nav is a simple horizontal text nav with active underline. No sidebar.
- Mobile nav keeps the same top position, switches labels to shorter text, and wraps when needed.
- Theme control is a small circular icon button at the far right of the nav.
- Color tokens are CSS variables:
  - dark background `#0d1117`
  - dark card `#161b22`
  - dark hover `#21262d`
  - dark text `#e6edf3`
  - muted text `#8b949e`
  - border `#30363d` / strong border `#444c56`
- Cards use `10px` to `12px` radius, 1px border, and restrained shadow.
- Buttons are compact, 8px radius, text-colored in dark mode; primary action uses foreground/background inversion.

## IP Page

- Interaction model: mostly static, with one search input and small action buttons.
- Top IP head combines large IP text, country/region, a score pill, and an inline search input.
- Main content is a two-column card grid on desktop; mobile stacks to one column.
- Key/value rows use dotted separators and right-aligned values.
- Status labels are small rounded pills.
- Additional sections are full-width cards with dense grids.

## DNS Page

- Interaction model: click-driven for test buttons and accordion disclosure sections.
- Feature strip uses four equal cards on desktop; mobile stacks vertically.
- Test controls are centered.
- Verdict card is full-width and colored by state: safe/warn/danger.
- Result table is compact with a bordered container.
- Reading cards are `details` accordions with right-side arrow.

## Responsive Behavior

- Desktop: centered 1000px container, cards/tables keep dense horizontal layout.
- Tablet: nav wraps; content retains two columns when there is enough space.
- Mobile around 390px: nav short labels, all grids stack, tables scroll horizontally.

## Local Implementation Mapping

- `/` maps to the node monitor dashboard.
- `/ip/` maps to node exit IP profile and score page.
- `/dns/` maps to node DNS leak page.
- The app keeps Proxy Check data and task controls, but adopts the reference visual shell and component density.
