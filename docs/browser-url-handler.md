# Browser URL handler troubleshooting

Clicking **"Open in desktop app"** on tidal.com sends a `tidal://` URL to the
OS. `tidalt setup` registers the handler at the XDG level, but some browsers
apply additional layers of protocol filtering on top of XDG that must be
configured separately.

---

## How the handler works

1. Browser navigates to `tidal://track/<id>` (or similar).
2. Browser delegates to `xdg-open "tidal://..."`.
3. `xdg-open` looks up the registered handler (`tidalt.desktop`) and runs
   `tidalt play tidal://track/<id>`.
4. `tidalt play` either forwards the URL to a running instance over D-Bus, or
   spawns a terminal with the full TUI.

You can verify steps 3–4 work correctly at any time by running:

```bash
xdg-open "tidal://track/497506611"
```

and then checking the log:

```bash
cat ~/.local/share/tidalt/play.log
```

If the log shows a successful invocation, the problem is in step 1 or 2 — the
browser is not handing the URL to `xdg-open`.

---

## Firefox / Librewolf

Firefox-family browsers block unknown URI schemes by default and do not
automatically delegate them to XDG. Two `about:config` preferences must be
set:

| Preference | Value |
|---|---|
| `network.protocol-handler.expose.tidal` | `false` |
| `network.protocol-handler.external.tidal` | `true` |

**Steps:**

1. Open `about:config` in the address bar and accept the warning.
2. Search for `network.protocol-handler.expose.tidal`. If it does not exist,
   create it as a **Boolean** and set it to `false`.
3. Search for `network.protocol-handler.external.tidal`. Create it as a
   **Boolean** and set it to `true`.
4. Restart the browser.

On the next `tidal://` click the browser will show a dialog asking which
application to use. Choose **tidalt** (or `xdg-open` if tidalt is not listed
directly). The choice is saved to the profile's `handlers.json` and will not
be asked again.

> **Librewolf note:** Librewolf ships with stricter defaults than upstream
> Firefox. Both preferences are required — setting only `expose` is not
> sufficient.

### Typing `tidal://` in the address bar does not work

Firefox treats unknown schemes typed directly into the address bar as search
queries rather than navigations. Use a link or bookmark instead; the protocol
handler is only invoked when the browser navigates to the URL programmatically
(e.g. via a link click or JavaScript redirect on tidal.com).

---

## Chromium / Chrome / Brave

Chromium-based browsers typically prompt the user the first time an unknown
scheme is encountered and remember the choice. If no prompt appears:

1. Go to **Settings → Privacy and security → Site settings → Additional
   content settings → Protocol handlers**.
2. Ensure the site (`tidal.com`) is not blocked.
3. Clear the blocked list if `tidal://` appears there.

---

## KDE Plasma

KDE uses its own protocol handler registry in addition to XDG. If `xdg-open`
works but the browser does not invoke it, run:

```bash
kreadconfig5 --file ~/.config/mimeapps.list --group "Default Applications" --key x-scheme-handler/tidal
```

If the output is empty, register manually:

```bash
xdg-mime default tidalt.desktop x-scheme-handler/tidal
kbuildsycoca5
```

---

## Still not working

1. Run `tidalt setup` again to ensure the desktop file and MIME registration
   are up to date.
2. Check `~/.local/share/tidalt/play.log` after attempting to open a URL.
3. Verify `xdg-mime query default x-scheme-handler/tidal` returns
   `tidalt.desktop`.
4. Verify the installed desktop file contains the full path to the binary:
   ```bash
   grep Exec ~/.local/share/applications/tidalt.desktop
   # Should output: Exec=/full/path/to/tidalt play %u
   ```
