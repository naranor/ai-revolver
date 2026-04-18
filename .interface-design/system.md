# AI Revolver: Tactical Design System

## Direction & Feel
**High-Precision Tactical Tool.** The interface must feel like a mission-critical control console or a ballistic computer. It is cold, precise, and information-dense. Zero fluff, high utility.

- **Theme Name**: "Tactical Ops"
- **Atmosphere**: Professional, mechanical, engineered.
- **Modes**: "Day Ops" (Light) / "Night Ops" (Dark).

## Core Primitives

### Colors (Tactical Palette)
- **Gunmetal Slate**: Core structure and backgrounds.
- **Brass/Copper (`--accent`)**: Primary interactive elements, priority indicators, and active states.
- **Phosphor Green (`--success`)**: Positive telemetry, live feeds, and successful "hits".
- **Safety Orange/Red (`--danger`)**: Rate limits, ballistic failures, and destructive actions.
- **Lead Gray**: Muted metadata and inactive trajectories.

### Depth & Surfaces
- **Strategy**: Borders-only. 
- **Radius**: 2px (Sharp edges).
- **Shadows**: None. Use high-contrast surface shifts (`surface-raised` vs `surface-inset`) to establish hierarchy.
- **Borders**: 1px solid. Use subtle opacity for default and strong contrast for active containers.

### Typography
- **UI Labels**: Inter (Medium/Semi-bold).
- **Data & Identifiers**: JetBrains Mono (or system monospace). All model IDs, latency numbers, and logs MUST be monospace.
- **Case**: Use Uppercase for headers and buttons to reinforce the mechanical feel.

## Component Patterns

### 1. The Panel (`.panel`)
The base container for all content. 
- **Structure**: `panel-header` (background-shift + border-bottom) and `panel-body`.
- **Spacing**: 20px body padding, 12px header padding.

### 2. Tactical Rail (Sidebar)
Fixed left navigation with high-density icons.
- **Active State**: Left border accent (3px) + text highlight.
- **Density**: Narrow width (240px) to maximize engagement area.

### 3. Mechanical Switches (Buttons)
- **Primary**: Brass background, white text.
- **Secondary**: Outlined gunmetal.
- **Ghost**: Pure text/icon, appearing only on hover or within dense grids.

### 4. Telemetry (Data Tables)
- **Headers**: Small caps, monospace, muted color.
- **Rows**: High contrast on hover, subtle borders.
- **Indicators**: Small 6px circular status lights with glow (box-shadow) for "Live" status.

## Spacing System
- **Base Unit**: 4px.
- **Scale**: 4, 8, 12, 16, 20, 24, 32, 40.
- **Layout Padding**: 32px (standard main content inset).
