# Installing gate4ai for a user — notes for an agent with a shell

You are an agent (OpenClaw, Claude Code, Hermes, …) asked to install gate4.ai on this computer or server. This page is for you: it is short, non-interactive and verifiable. The human pages are https://gate4.ai/install and https://github.com/gate4ai/gate4ai#readme.

## 1. Lower the stakes first (optional, recommended on a shared server)

gate4ai is the guard between agents and the computer, so it should not share an OS user with the agent it guards. On a server either create a separate user (`useradd -m gate4ai && loginctl enable-linger gate4ai`, install as that user) or run the container image (section 5). Installing under your own user also works; then tell the user that you and gate4ai share one user and the policy, not the OS, is the boundary.

## 2. Verify what you download

The installer is https://gate4.ai/install.sh. It downloads the latest GitHub release of `gate4ai/gate4ai`, checks sha256 checksums and, when `cosign` is installed, the keyless signature made only by the release workflow on a tag:

```sh
cosign verify-blob --certificate checksums.txt.pem --signature checksums.txt.sig \
  --certificate-identity-regexp '^https://github.com/gate4ai/gate4ai/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
gh attestation verify gate4ai_<version>_linux_amd64.tar.gz -R gate4ai/gate4ai   # SLSA provenance
```

Releases, SECURITY.md and the OpenSSF Scorecard are on https://github.com/gate4ai/gate4ai. The repository is young; the signatures and attestations above are what to trust, not the star count.

## 3. Install without a terminal

```sh
curl -fsSL https://gate4.ai/install.sh | GATE4AI_NO_TRAY=1 sh
```

The installer asks nothing. What it does and the variables that change it:

- Puts `gate4ai` into `~/.local/bin` (`GATE4AI_INSTALL_DIR`), no root.
- **Turns on access from the internet** (the daemon registers this computer on `relay.gate4.ai` and gets `https://<id>.relay.gate4.ai`). This is expected, not a leak: the relay sees only a random device id and encrypted traffic (TLS passthrough, the certificate is issued to this computer), no data, no account; nothing else leaves the computer, there is no telemetry and no update check. The Web UI on that address asks the password; API of the owner is not there. The project counts its users by these registrations. `GATE4AI_NO_RELAY=1` — install with it off (the daemon never registers); later `gate4ai relay off` / `gate4ai relay on`. Details: section 7.
- Turns on the autostart (`gate4ai service install`: systemd --user / launchd); if that is impossible it starts the daemon for this session. `GATE4AI_NO_START=1` — neither.
- **Chrome for the browser of agents** (`gate4ai browser setup`): finds Chrome in PATH, snap, flatpak, Playwright and Puppeteer caches (`GATE4AI_CHROME` — your own path), checks its shared libraries with `ldd`, Xvfb and fonts. Without a terminal it installs what is missing by itself when it is root or `sudo -n` works; otherwise it downloads Chrome for Testing into gate4ai's data folder (no root) and prints exactly which packages still need root, e.g. `To run Chrome, install: libnss3 libgbm1.` — **tell the user that list verbatim**. `GATE4AI_INSTALL_DEPS=1` — install without asking even at a terminal; `GATE4AI_NO_BROWSER=1` — skip Chrome now.
- Ends with three lines on stdout:

  ```
  Web UI: http://127.0.0.1:7431/
  Password: xxxxx-xxxxx-xxxxx-xxxxx   (generated, shown until the first sign-in — save it; …)
  From the internet: https://<id>.relay.gate4.ai/ui/
  ```

  The internet address appears after the device registers on the relay and gets a certificate; the installer waits for it up to `GATE4AI_UI_WAIT` (default `3m`, `0` — do not wait). If the certificate could not be issued yet, the address works on a temporary self-signed certificate and the line says so: the browser warns about it, gate4ai replaces it with the real one by itself. Under the address a line says what the relay sees and how to turn it off (`gate4ai relay off`; at install `GATE4AI_NO_RELAY=1`).

## 4. What to tell the user

Pass the three lines to the user in the chat. The internet address opens from their phone or laptop — do not suggest an ssh tunnel or port forwarding. Ask them to sign in, change the password (Settings → Signing in) and turn on Telegram confirmation there: the password went through a chat, and gate4ai reminds about it on every page until they do. If Chrome needs packages, give them the exact `To run Chrome, install: …` list and the command for root.

The same data any time, as text or JSON:

```sh
gate4ai ui --print            # the three lines (owner command)
gate4ai ui --print --json     # url, password, remote_url, remote_cert (ok | temporary), remote_phase, remote_error
gate4ai browser setup --dry-run --json   # chrome, missing_libraries, needs (packages, command), summary, needs_root
gate4ai doctor
```

If `gate4ai ui` answers `agent_required` (exit 7), gate4ai recognised you as an agent on the owner's socket: give the user the command to run instead of looking for another way.

## 5. Container image

For a server without root or without Chrome's libraries:

```sh
docker run -d --name gate4ai --restart unless-stopped --security-opt seccomp=unconfined \
  -v gate4ai-data:/data ghcr.io/gate4ai/gate4ai:latest
docker exec gate4ai gate4ai ui --print
```

The image has Chrome, Xvfb and fonts; it is signed with cosign (`cosign verify ghcr.io/gate4ai/gate4ai:<version> …`, see SECURITY.md). The Web UI inside the container listens on loopback: use the internet address, or `--network host`.

## 6. Connect yourself

`gate4ai skill > <your skills folder>/gate4ai/SKILL.md`, then run any `gate4ai` command: the first one answers `pairing_required` with a code — show it to the user, they approve it in the Web UI or Telegram. From another OS user or computer: `GATE4AI_URL=https://<id>.relay.gate4.ai`.

## 7. What leaves this computer

Nothing but the relay connection, and only what the tunnel needs:

- **Registration** (`POST relay.gate4.ai/api/devices`, once): the relay generates a random device id and a token; the computer sends nothing about itself — no hostname, user, version or IP in the state beyond what any TCP connection shows.
- **Certificate** for `<id>.relay.gate4.ai`: issued by Let's Encrypt to this computer (ACME DNS-01; the relay only publishes the TXT record), the private key never leaves it.
- **Tunnel**: an outgoing WebSocket; incoming TLS is passed through to the computer by SNI, so the relay sees ciphertext only. A hash of the journal checkpoint (no content) is sent as a witness.
- **No telemetry, no update check, no analytics.** The project counts its users by relay registrations (`gate4ai_relay_devices` on the relay), that is all.

Turn it off at any time: `gate4ai relay off` (the device id is kept, `gate4ai relay on` brings the same address back without a new certificate), the **Settings → Internet access** page, or `GATE4AI_NO_RELAY=1` at install. Without the relay cloud assistants (Claude.ai, ChatGPT) and the phone cannot reach this computer; local agents work as before. The relay is open source (AGPL-3.0, `relay/` in the repository) and can be self-hosted: `gate4ai relay register --url https://relay.example.org`.
