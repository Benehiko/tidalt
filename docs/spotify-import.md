# Importing from Spotify

tidalt can take a Spotify **track** or **playlist** URL, find each song on Tidal,
and either build a Tidal playlist from the matches or load them straight into the
queue. Songs that can't be found on Tidal are listed with a **not available**
tag so you can see exactly what didn't carry over.

No Spotify account, login, or developer credentials are required.

## How to use it

1. Open the command palette with `:` or `Ctrl+P`.
2. Choose **Import from Spotify…**.
3. Paste a Spotify URL and press `Enter`. Accepted forms:
   - `https://open.spotify.com/track/<id>`
   - `https://open.spotify.com/playlist/<id>`
   - `spotify:track:<id>` / `spotify:playlist:<id>`
   - Locale-prefixed web links (e.g. `.../intl-de/track/<id>`) and `?si=…`
     tracking query strings are handled.
4. tidalt resolves the Spotify source and searches Tidal for each track. The
   **review** screen shows every source track:
   - `✓` — matched, with the Tidal track title.
   - `✕ … not available` — no Tidal match was found.
   The header shows how many of the total tracks matched.
5. Press `Enter` to choose a destination:
   - **Create Tidal playlist** — creates a new playlist in your Tidal account,
     named after the Spotify playlist (or track), containing every matched track.
   - **Load into queue** — loads the matched tracks into the live queue and starts
     playing, without touching your Tidal account.

`Esc` backs out of any step.

### Quick path

You can also paste a Spotify URL directly into the **Search** box (or pass it as a
command-line argument). On this quick path the matched Tidal tracks are loaded
into the queue and unmatched tracks are silently dropped — use the command-palette
import flow above if you want to review unmatched tracks or create a playlist.

## How matching works

- For a **playlist**, tidalt reads the title and artist of every track and searches
  Tidal with `title artist`, taking the top result as the match.
- For a **single track**, only the combined title string is available (see below),
  so the match is fuzzier than for playlists.
- Matching is a best-effort, top-result heuristic. A `✓` means *a* Tidal track was
  found for that search, not that it is guaranteed to be the exact same recording
  (a different remaster or version may be picked). Review the list before importing.

## How Spotify data is read (and its limitations)

Spotify locked down its Web API in February 2026: the Client Credentials flow no
longer returns metadata, and even a full user-OAuth token can only read playlists
the signed-in user owns. There is therefore no official, credential-free way to
read an arbitrary public playlist. tidalt uses the two remaining no-login paths:

- **Track URLs** are resolved through Spotify's official **oEmbed** endpoint, which
  returns the track's title only — there is no separate, structured artist field,
  which is why single-track matches are fuzzier than playlist matches.
- **Playlist URLs** are read by parsing Spotify's public **embed page**, which
  server-renders the full track list (title + artist) in an embedded JSON blob.

The playlist path is unofficial and depends on Spotify's page layout. If Spotify
changes that layout, the import will fail with a clear "layout may have changed"
error rather than producing wrong results; the parsing is isolated in the
`internal/spotify` package and covered by a regression test so breakage is caught
early. This feature reads only public pages and creates nothing on Spotify's side.
