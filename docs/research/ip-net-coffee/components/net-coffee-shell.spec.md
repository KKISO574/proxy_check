# NetCoffeeShell Specification

## Overview

- Target file: `frontend/src/main.tsx`
- Screenshot: `docs/design-references/ip-net-coffee/ip-desktop.png`
- Interaction model: click-driven navigation and theme toggle.

## DOM Structure

- `.nc-page`
  - `.nc-container`
    - `.nc-nav`
      - nav buttons
      - `.theme-toggle`
    - page content
    - `.nc-footer`

## Computed Style Targets

- Container max width: `1000px`.
- Container padding: desktop `24px 20px`; mobile `16px 14px`.
- Nav display: `flex`, gap `20px`, font size `14.4px`.
- Active nav: border bottom `2px solid currentColor`, font weight `700`.
- Theme button: `28px` square, circular, border `1px solid var(--border-strong)`.

## Responsive Behavior

- Desktop: full nav labels.
- Mobile: short nav labels and wrapped nav.

## Text Content

- `节点监控`
- `IP 画像`
- `DNS 泄露检测`
