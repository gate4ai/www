#!/bin/sh
# gate4.ai installer for macOS and Linux.
#
#   curl -fsSL https://gate4.ai/install.sh | sh
#
# Options (environment variables):
#   GATE4AI_VERSION=v0.1.1    install a specific version (default: latest)
#   GATE4AI_INSTALL_DIR=DIR   install folder (default: ~/.local/bin)
#   GATE4AI_NO_TRAY=1         do not install the tray icon app (headless servers)
#   GATE4AI_NO_START=1        do not turn on the autostart. By default the
#                             installer runs gate4ai service install: the daemon
#                             starts now and at every login (systemd --user /
#                             launchd), the tray icon at every login.
#   GATE4AI_NO_OPEN=1         do not open the Web UI after installing. By default,
#                             in a graphical session (Linux: DISPLAY or
#                             WAYLAND_DISPLAY; macOS outside SSH) and outside CI
#                             (CI not set), the installer runs gate4ai ui: the
#                             browser opens the Web UI (sign in with the
#                             generated password the installer prints, then the
#                             setup wizard).
#   GATE4AI_INSTALL_DEPS=1    install what the browser of agents needs without
#                             asking: Google Chrome, on Linux without a screen
#                             also Xvfb (a virtual screen) and fonts (with sudo).
#                             By default the installer checks them, prints the
#                             commands and, at a terminal, asks first.
#   GATE4AI_NO_BROWSER=1      do not check Chrome now (the browser is built into
#                             gate4ai; check later with gate4ai browser setup).
#
# Downloads the release from GitHub, checks sha256 checksums (and the cosign
# signature when cosign is installed: made by release.yml of gate4ai/gate4ai
# on a v* tag) and only then installs. Never touches your configuration or
# data and calls sudo only to install Chrome or Xvfb after you agree
# (gate4ai browser setup).
#
# The whole script is one function called on the last line: a download cut
# short by the network does not run half of it.

main() {
  set -eu

  SITE="${GATE4AI_SITE:-https://gate4.ai}"
  REPO="gate4ai/gate4ai"
  DOWNLOAD_BASE="${GATE4AI_DOWNLOAD_BASE:-https://github.com/$REPO/releases/download}"
  RELEASES="https://github.com/$REPO/releases"
  INSTALL_DIR="${GATE4AI_INSTALL_DIR:-$HOME/.local/bin}"
  NO_TRAY="${GATE4AI_NO_TRAY:-}"
  NO_START="${GATE4AI_NO_START:-}"
  NO_OPEN="${GATE4AI_NO_OPEN:-}"
  INSTALL_DEPS="${GATE4AI_INSTALL_DEPS:-}"
  NO_BROWSER="${GATE4AI_NO_BROWSER:-}"

  say() { printf '%s\n' "$*"; }
  fail() {
    printf 'gate4ai install: %s\n' "$*" >&2
    exit 1
  }

  # --- platform ---
  case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) fail "unsupported OS $(uname -s). Download manually: $RELEASES" ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) fail "unsupported CPU $(uname -m). Download manually: $RELEASES" ;;
  esac

  # --- tools ---
  # Releases are fetched over HTTPS only; plain HTTP is allowed just for a
  # mirror set explicitly in GATE4AI_DOWNLOAD_BASE (tests).
  proto='=https'
  case "$DOWNLOAD_BASE" in
    http://*) proto='=http,https' ;;
  esac
  if command -v curl >/dev/null 2>&1; then
    fetch() { curl -fsSL --proto "$proto" --tlsv1.2 --retry 3 -o "$2" "$1"; }
    fetch_text() { curl -fsSL --retry 2 --max-time 15 "$1"; }
  elif wget --version 2>/dev/null | grep -q 'GNU Wget'; then
    # The same rules as curl: TLS 1.2 or newer and HTTPS only. wget has no option
    # against a redirect to http (--https-only is for recursive mode), so the
    # redirects are read from the response headers (-S) and such a download is
    # thrown away; nothing is installed yet at that point. BusyBox wget has no
    # such options, so it is not used.
    fetch() {
      if [ "$proto" = '=https' ]; then
        case "$1" in https://*) ;; *) return 1 ;; esac
      fi
      wget -q -S --secure-protocol=TLSv1_2 --tries=3 -O "$2" "$1" 2>"$2.hdr" || {
        rm -f "$2.hdr"
        return 1
      }
      if [ "$proto" = '=https' ] && grep -qi '^[[:space:]]*Location:[[:space:]]*http:' "$2.hdr"; then
        rm -f "$2" "$2.hdr"
        printf 'gate4ai install: %s redirects to plain http\n' "$1" >&2
        return 1
      fi
      rm -f "$2.hdr"
    }
    fetch_text() { wget -q -T 15 -O - "$1"; }
  else
    fail "curl (or GNU wget) is required"
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256() { sha256sum "$1" | awk '{print $1}'; }
  elif command -v shasum >/dev/null 2>&1; then
    sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
  else
    fail "sha256sum or shasum is required"
  fi
  command -v tar >/dev/null 2>&1 || fail "tar is required"

  # --- version ---
  valid_tag() {
    case "$1" in
      v[0-9]*) ;;
      *) return 1 ;;
    esac
    case "$1" in
      *[!A-Za-z0-9.-]*) return 1 ;;
    esac
    return 0
  }
  tag="${GATE4AI_VERSION:-}"
  if [ -z "$tag" ]; then
    tag="$(fetch_text "$SITE/latest" 2>/dev/null | tr -d ' \r\n' || true)"
    if ! valid_tag "$tag"; then
      # The site is unavailable: ask GitHub directly.
      tag="$(fetch_text "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null |
        sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1 || true)"
    fi
  fi
  case "$tag" in
    v*) ;;
    ?*) tag="v$tag" ;;
  esac
  valid_tag "$tag" || fail "could not find the latest version; set GATE4AI_VERSION, e.g. GATE4AI_VERSION=v0.1.1"
  ver="${tag#v}"

  tmp="$(mktemp -d 2>/dev/null || mktemp -d -t gate4ai)"
  trap 'rm -rf "$tmp"' EXIT INT TERM

  say "Installing gate4ai $tag for $os/$arch"

  # --- download and verify everything before installing anything ---
  verify_sig() { # verify_sig <checksums file>
    if command -v cosign >/dev/null 2>&1; then
      fetch "$DOWNLOAD_BASE/$tag/$1.sig" "$tmp/$1.sig" || fail "cannot download $1.sig"
      fetch "$DOWNLOAD_BASE/$tag/$1.pem" "$tmp/$1.pem" || fail "cannot download $1.pem"
      cosign verify-blob \
        --certificate "$tmp/$1.pem" --signature "$tmp/$1.sig" \
        --certificate-identity-regexp "^https://github.com/$REPO/\.github/workflows/release\.yml@refs/tags/v" \
        --certificate-oidc-issuer https://token.actions.githubusercontent.com \
        "$tmp/$1" >/dev/null 2>&1 || fail "signature of $1 is NOT valid; nothing was installed"
      say "  signature of $1: ok (cosign)"
    fi
  }
  get_checked() { # get_checked <archive> <checksums file>
    fetch "$DOWNLOAD_BASE/$tag/$1" "$tmp/$1" || fail "cannot download $1 (is $tag released for $os/$arch? $RELEASES)"
    want="$(awk -v f="$1" '$2 == f || $2 == "*" f {print $1}' "$tmp/$2" | head -n 1)"
    [ -n "$want" ] || fail "$1 is not listed in $2"
    got="$(sha256 "$tmp/$1")"
    [ "$want" = "$got" ] || fail "checksum of $1 does NOT match; nothing was installed"
    say "  $1: checksum ok"
  }

  fetch "$DOWNLOAD_BASE/$tag/checksums.txt" "$tmp/checksums.txt" || fail "cannot download checksums.txt for $tag"
  verify_sig checksums.txt
  core="gate4ai_${ver}_${os}_${arch}.tar.gz"
  get_checked "$core" checksums.txt

  tray=""
  if [ -z "$NO_TRAY" ]; then
    tray="gate4ai-tray_${ver}_${os}_${arch}.tar.gz"
    sums="checksums.txt"
    if [ "$os" = darwin ]; then
      sums="tray-darwin-checksums.txt"
      fetch "$DOWNLOAD_BASE/$tag/$sums" "$tmp/$sums" || fail "cannot download $sums"
      verify_sig "$sums"
    fi
    get_checked "$tray" "$sums"
  fi

  mkdir -p "$tmp/core"
  tar -xzf "$tmp/$core" -C "$tmp/core"
  [ -f "$tmp/core/gate4ai" ] || fail "gate4ai is missing in $core"
  if [ -n "$tray" ]; then
    mkdir -p "$tmp/tray"
    tar -xzf "$tmp/$tray" -C "$tmp/tray"
    tray_bin="$tmp/tray/gate4ai-tray"
    [ -f "$tray_bin" ] || tray_bin="$tmp/tray/gate4ai-tray_${ver}_${os}_${arch}/gate4ai-tray"
    [ -f "$tray_bin" ] || fail "gate4ai-tray is missing in $tray"
  fi

  # --- install (atomic rename: works even if gate4ai is running) ---
  mkdir -p "$INSTALL_DIR" || fail "cannot create $INSTALL_DIR; set GATE4AI_INSTALL_DIR to a folder you can write to"
  place() { # place <file> <name>
    cp "$1" "$INSTALL_DIR/.$2.new" || fail "cannot write to $INSTALL_DIR; set GATE4AI_INSTALL_DIR to a folder you can write to"
    chmod 755 "$INSTALL_DIR/.$2.new"
    mv -f "$INSTALL_DIR/.$2.new" "$INSTALL_DIR/$2"
  }
  place "$tmp/core/gate4ai" gate4ai
  [ -z "$tray" ] || place "$tray_bin" gate4ai-tray

  say ""
  say "Installed: $("$INSTALL_DIR/gate4ai" --version 2>/dev/null || echo "gate4ai $tag")"
  say "  $INSTALL_DIR/gate4ai"
  [ -z "$tray" ] || say "  $INSTALL_DIR/gate4ai-tray"

  case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
      say ""
      say "$INSTALL_DIR is not in your PATH. Add it:"
      case "${SHELL:-}" in
        */zsh) rc="$HOME/.zshrc" ;;
        */bash) rc="$HOME/.bashrc" ;;
        */fish) rc="" ;;
        *) rc="$HOME/.profile" ;;
      esac
      if [ -n "$rc" ]; then
        say "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> $rc && . $rc"
      else
        say "  fish_add_path $INSTALL_DIR"
      fi
      ;;
  esac

  # gate4ai is called by its full path: INSTALL_DIR may be missing from PATH.
  gate="$INSTALL_DIR/gate4ai"

  # --- the browser of agents: Chrome, on a server also a virtual screen ---
  # gate4ai browser setup checks them and installs what is missing only after
  # a yes (its question reads the terminal: stdin is this script under curl | sh).
  say ""
  if [ -n "$NO_BROWSER" ]; then
    say "Chrome is not checked (GATE4AI_NO_BROWSER). Agents get browser_unavailable until it is installed: gate4ai browser setup"
  else
    say "The browser of agents (gate4ai browser setup):"
    browser_ok=1
    if [ -n "$INSTALL_DEPS" ]; then
      GATE4AI_OUTPUT=table "$gate" browser setup --yes || browser_ok=""
    elif [ -t 2 ] && [ -z "${CI:-}" ] && (exec </dev/tty) 2>/dev/null; then
      GATE4AI_OUTPUT=table "$gate" browser setup </dev/tty || browser_ok=""
    else
      GATE4AI_OUTPUT=table "$gate" browser setup --dry-run || browser_ok=""
    fi
    [ -n "$browser_ok" ] || say "Agents get browser_unavailable until Chrome is installed: gate4ai browser setup"
  fi
  running=""
  if [ -n "$NO_START" ]; then
    # An update: a daemon that is already running keeps the old binary until it restarts.
    if "$gate" daemon status >/dev/null 2>&1; then
      if "$gate" daemon restart >/dev/null 2>&1; then
        running=1
        say "The running gate4ai daemon was restarted with the new version."
      else
        say "Restart the running daemon to use the new version: gate4ai daemon restart"
      fi
    fi
  else
    # The autostart; it also restarts a daemon that is already running (an update).
    say ""
    no_tray_flag=""
    [ -n "$tray" ] || no_tray_flag="--no-tray"
    # shellcheck disable=SC2086
    if "$gate" service install $no_tray_flag >/dev/null; then
      running=1
      say "The gate4ai daemon is running and starts at every login (gate4ai service status)."
    else
      say "Could not set up the autostart: gate4ai ui starts the daemon for this session."
    fi
  fi

  # The Web UI shows the setup wizard: open it right away in a graphical session.
  graphical=""
  case "$os" in
    linux) [ -z "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ] || graphical=1 ;;
    darwin) [ -n "${SSH_CONNECTION:-}${SSH_TTY:-}" ] || graphical=1 ;;
  esac
  # The Web UI password is generated by the daemon at its first start: gate4ai ui
  # prints it (stderr) until the first sign-in, so the owner sees it here even
  # without a graphical session. Lost it: gate4ai ui --reset-password.
  say ""
  if [ -n "$running" ] && [ -n "$graphical" ] && [ -z "${CI:-}" ] && [ -z "$NO_OPEN" ] &&
    "$gate" ui; then
    say "The Web UI is open in your browser: sign in with the password above, then the setup wizard starts. Open it again any time: gate4ai ui"
  elif [ -n "$running" ] && "$gate" ui --print >/dev/null; then
    say "Open the address above in a browser on this computer and sign in with the password above (save it; it is shown until the first sign-in). Then the setup wizard starts."
  else
    say "Next: gate4ai ui   # prints the Web UI address and password, opens the setup wizard in your browser"
  fi
}

main "$@"
