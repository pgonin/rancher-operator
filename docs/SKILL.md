---
name: rancher-ui-experience-guardrails
description: Activates automatically when writing code, designing interfaces, or laying out components matching the Rancher Design System, Dashboard, and UI Extensions architecture.
---

# Rancher UI & UX System Guardrails

Use this skill to guide the creation, refactoring, and generation of web application interfaces adhering strictly to the Rancher Dashboard framework (`@rancher/components` and `@rancher/shell`).

## 1. Core Visual Identity & Theming

Rancher uses a dense, highly professional enterprise interface tailored for infrastructure management. It relies heavily on absolute alignment, high-contrast states, and standard layout modules.

### Color Tokens & Semantic States
Do not invent random hex codes. Rancher's color logic maps precisely to structural resource states:
*   **Primary Action / Interactive:** `--primary` (Rancher Corporate Blue / Teal accents).
*   **Success (Active, Healthy, Up):** Green (`--success`) -> Used for running pods, active clusters.
*   **Warning (Degraded, Reconfiguring):** Orange (`--warning`) -> Used for updating, scaling, or transient issues.
*   **Error (Failed, Down):** Red (`--error`) -> Used for stopped containers, bad configurations, unhealthiness.
*   **Info / Neutral (Transitioning, Initializing):** Blue/Gray (`--info`) -> Used for standard logs, neutral notices.

### Dark & Light Mode Support
All components generated must support Rancher's strict dark/light mode context. Always use CSS variables (`var(--body-bg)`, `var(--border)`, `var(--text-default)`) instead of hardcoded background utilities.

---

## 2. Component Construction Rules

When writing UI elements or full views, map everything directly to the foundational structural atoms found in `@rancher/components`.

### Layout Modules
*   **Dashboard Grid:** Top stats or resource metrics must use standard `Row` and `Col` layouts or grid cards tracking core system metrics (CPU, Memory, Storage).
*   **Resource Headers:** Every main management view must feature a standard header title, sub-description, and a top-right contextual placement for action buttons (e.g., "Create", "Import").

### Form Controls & Inputs
Rancher forms require precise structured validation boundaries.
*   **Form Groups:** Group inputs within labeled blocks that support error state passing (`:error="validationError"`).
*   **Checkboxes & Radios:** Use the custom Rancher implementations that allow descriptive labels right inside the toggle block.
*   **Advanced Selectors:** Use standard select drop-downs that include support for tags, search filters, and grouped categorizations.

### Tables & Data Displays
Data representation is the heart of Rancher.
*   **The Resource Table (`SortableTable`):** Tables must support column sorting, global text filtering, checkbox multi-selection, and a primary column featuring a status icon/badge on the extreme left.
*   **Badges & Indicators:** Use semantic state indicators (`BadgeState`) to show the status of resources. Never present a status as raw un-styled text.

---

## 3. Operational UX Guidelines

*   **Atomic Isolation:** Components must manage local state reactively using Vue 3 patterns without relying on monolithic side effects. Avoid tight binding to specific APIs directly inside foundational UI atoms.
*   **Responsive Optimization:** Keep data layouts density-aware. Space is valuable; use compact tables, expandable rows, and sliding side-drawers (`Wizard` / `Panel` overlays) for multi-step tasks instead of disruptive full-page redirects.
*   **Progressive Form Disclosures:** For heavy configurations (like YAML generation or cluster settings), split workflows using clean sequential tabs or step wizards.

## 4. Prompting Directives

When this skill is active, you must output code that implements these specific paradigms:
1.  Prefer building forms and tables that accept raw configuration schemas (resembling Kubernetes resource definitions or structured YAML manifests).
2.  Incorporate state tracking and loading indicators gracefully onto structural action triggers.
3.  Ensure all generated files are structured explicitly to match the architecture of a Rancher UI extension or dashboard view plugin.

