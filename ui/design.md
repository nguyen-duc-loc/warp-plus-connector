# Terminal overview design

The confirmed source is spec 0010, Overview and saved reads. You see a compact utility screen using your terminal font, foreground, and background. The active section has a cyan accent and a visible `>` marker. Status always has a text label.

## Composition

The fixed header names WPC, Overview, the selected collection, local date, UTC offset, and refresh state. The scrolling body holds Collection, Recent work, Schedule, and Service in that order. The fixed footer lists keyboard shortcuts. Help returns to the same focus and scroll position.

## Presentation

Values and labels align on character cells. At 100 columns, recent work uses a table. At 60 through 99 columns, rows wrap into labeled blocks. Below 60 columns or 16 rows, a resize notice replaces the body. There are no images, downloaded fonts, animations, or custom background colors. Presentation constants live in `src/theme.ts`.

## States and access

Loading, missing, empty, unavailable, and stale data have separate messages. Counts from failed reads never become zero. Focus and all status meaning remain visible when color is disabled. Every action uses the keyboard. Dynamic text is escaped before layout.
