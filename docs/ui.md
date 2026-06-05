# The TIDALT interface

TIDALT's terminal interface is built around a persistent **sidebar** on the left
and a **main content pane** on the right, with a now-playing bar and a
context-aware key-hint bar pinned along the bottom. This document is a tour of
how the pieces fit together.

## Layout

```
┌ sidebar ┬──────────── main pane ─────────────┐
│ NOW     │ ╭─ QUEUE · Late Night · synced ───╮ │
│ ♪ …     │ │ 1  May These Noises       1:18  │ │
│ ≣ Queue │ │ 2  Hell Above ♥           3:32  │ │
│ LIBRARY │ │ …                               │ │
│ ≡ …     │ ╰─────────────────────────────────╯ │
├─────────┴─────────────────────────────────────┤
│ ▪ ▶ Tangled In The Great Escape   2:32 / 5:56  │  ← now-playing bar
├────────────────────────────────────────────────┤
│ j/k Move │ h/l Pane │ ↵ Play │ o Actions │ …    │  ← key hints
└────────────────────────────────────────────────┘
```

Focus lives in one of two zones at a time: the **sidebar** (you're choosing a
section) or the **main pane** (you're acting on the section's content). `h` and
`l` move focus between them; `Enter` on a sidebar item also jumps into the pane.

On narrow terminals the sidebar collapses to icons only (below ~62 columns) and
is hidden entirely below ~40 columns so the main pane stays usable.

## Sections

The sidebar groups every destination:

- **NOW**
  - **Queue** — the live playback queue with the cover art of the *hovered* track
    on the right (Kitty graphics where supported, Unicode block-art otherwise);
    moving the cursor previews each track's cover. See *The queue* below.
- **LIBRARY**
  - **Playlists** — a two-column view: the playlist index on the left, the
    selected playlist's tracks on the right. `Enter`/`→` opens a playlist;
    `Enter` in the detail loads it into the queue and starts playing.
  - **Songs / Artists / Albums** — your Tidal favorites, each as its own list.
    From an artist you can drill into an album to see its tracks.
  - **Recently Played** — recently played tracks, most recent first, persisted
    across sessions.
- **TIDAL**
  - **Daily Mixes** — your Tidal mixes; `Enter` loads a mix into the queue.
  - **Search** — see *Search* below.
- **SETTINGS**
  - **Themes** — the color-scheme picker (see *Themes*).

## The queue (hybrid model)

The queue is your **live workspace**. It is never a saved playlist by itself —
but it remembers where its contents came from, shown in the panel title:

| State | Header | Meaning |
| ----- | ------ | ------- |
| Loaded from a saved playlist, untouched | `QUEUE · <name> · synced` (green) | matches the saved playlist |
| Loaded from a playlist, then edited | `QUEUE · <name> · edited — S save` (amber) | you've added/reordered tracks |
| Built from radio or ad-hoc adds | `QUEUE · radio · unsaved — S save` (amber) | nothing saved yet |

Editing the queue (play-next, add-to-queue) **never** rewrites the saved
playlist it came from. Press `S` (or use the command palette's
*Save queue as playlist…*) to commit the current queue as a brand-new playlist;
*Save queue to existing playlist…* appends it to one you already have. A green
toast confirms the save.

`x` removes the selected track from the queue and `C` clears it (the current
track keeps playing in both cases).

## The action sheet

Press `o` on any track to open a contextual popup of actions, so the same set of
operations is available everywhere — queue, search results, playlist detail,
history:

- ▸ Play now · ⏭ Play next (`n`) · ＋ Add to queue (`e`)
- ≡ Add to playlist… · ∿ Start radio from this (`r`)
- ♫ Go to artist (`a`) · ⊞ Go to album (`A`)
- ♥ Favorite (`f`) · ⎘ Copy Tidal link (`c`)

Each action also has the single-key shortcut shown in parentheses, usable
directly on the list without opening the sheet.

## The command palette

Press `:` or `Ctrl+P` to open a fuzzy command palette. Type to filter, `j`/`k`
(or `Ctrl+J`/`Ctrl+K`) to move, `Enter` to run. It groups into:

- **ACTIONS** — *Save queue as playlist…*, *Save queue to existing playlist…*
- **JUMP TO** — every sidebar section

## Search

Search queries Tidal across categories and groups the results into **SONGS**,
**ARTISTS**, and **ALBUMS**. The cursor moves across all groups; `Enter` does the
natural thing for the highlighted row — play a song, drill into an artist's
discography, or load an album into the queue. Track rows also accept the action
sheet and the `f`/`r` shortcuts.

## Themes

The **Themes** section is a live theme picker. Eight schemes ship built in —
TIDALT (the default slate look), Catppuccin Mocha, Tokyo Night, Gruvbox Dark,
Nord, Rosé Pine, Dracula, and Amber CRT — plus an **Auto — match terminal**
option that follows your terminal's own colors.

Moving the cursor with `j`/`k` **previews** the scheme by re-theming the whole
interface instantly; `Enter` applies and saves it, and `Esc` cancels the preview
and reverts. The chosen theme persists across launches. `t` cycles the theme
from anywhere in the app.

## Client mode

When a second TIDALT instance starts while one is already running, it launches in
**client mode**: it forwards playback to the running instance over D-Bus instead
of opening the audio device itself. A client instance is tinted with a
steel-blue accent and shows a `⇄ CLIENT` badge in the sidebar header, so it's
always clear which instance owns playback. The chosen theme still applies; only
the focus accent changes. See [client-server.md](client-server.md) for details.
