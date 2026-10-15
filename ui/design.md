# Terminal overview design

The confirmed source is spec 0010, Overview and saved reads. You see a compact utility screen using your terminal font, foreground, and background. The active section has a cyan accent and a visible `>` marker. Status always has a text label.

## Composition

The fixed header names WPC, Overview, the selected collection, local date, UTC offset, and refresh state. The scrolling body holds Collection, Recent work, Schedule, and Service in that order. The fixed footer lists keyboard shortcuts. Help returns to the same focus and scroll position.

## Presentation

Values and labels align on character cells. At 100 columns, recent work uses a table. At 60 through 99 columns, rows wrap into labeled blocks. Below 60 columns or 16 rows, a resize notice replaces the body. There are no images, downloaded fonts, animations, or custom background colors. Presentation constants live in `src/theme.ts`.

## States and access

Loading, missing, empty, unavailable, and stale data have separate messages. Counts from failed reads never become zero. Focus and all status meaning remain visible when color is disabled. Every action uses the keyboard. Dynamic text is escaped before layout.

## Results, details, and export

Spec 0011 extends this language with a fixed Results header, exact counts, snapshot time, applied filters, a candidate body, and keyboard shortcuts. Wide terminals align mark, full ID, latest outcome, definitive result, evidence time, and freshness. Compact terminals use three labeled lines per candidate. The focus marker remains visible without color. Navigation keeps only a bounded page in memory.

Filters use three groups of checkboxes, with a separate draft until you apply it. Marks belong to the current view. A changed filter clears them. A failed refresh labels the retained view and preserves its original evaluation and position.

Details show Results and Details times separately. Summary, Sources, and Attempts have keyboard tabs and independent positions. Missing history remains unavailable. A revealed key is quoted, scrollable text and disappears on help, refresh, exit, or an unusably small terminal.

Export begins with a selection, format, disclosure choice, and an empty destination. Review shows the fixed count, filters, evaluation, and resolved path. Back receives initial focus. Preparation shows a phase and a count, with cancel, help, resize, and quit available. Outcomes distinguish a created file, known absence of publication, and an uncertain result. Color never carries these meanings alone.
