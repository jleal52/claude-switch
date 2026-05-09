# claude-switch

Native wrapper + central server + web frontend to drive multiple `claude` CLI sessions remotely, across machines and accounts.

Designed in four subsystems (each with its own spec + plan):

1. **Wrapper PTY** — Go binary on the user's machine; hosts N `claude` PTY sessions and streams them over a single outbound WebSocket.
2. **Server** — central relay; public API, session catalog, authentication.
3. **Frontend** — browser UI that connects to the server and exposes terminals, transcripts, and session management.
4. **Multi-account** — profiles per account, credential isolation, account-aware routing.

Current status: design phase for subsystem 1. See `docs/superpowers/specs/`.

---

## Installing the wrapper

> **Distribution status (heads up):** macOS has a **Homebrew tap** (Intel + Apple Silicon). Linux and Windows still install from the **GitHub Release** tarballs/zips — no Scoop bucket, `winget` manifest, or `apt`/`yum`/`pacman` repo yet. Binaries are not code-signed or notarized.

Releases live at:

```
https://github.com/jleal52/claude-switch/releases
```

Each release publishes:

- `claude-switch_<version>_linux_amd64.tar.gz`, `..._linux_arm64.tar.gz`
- `claude-switch_<version>_darwin_amd64.tar.gz`, `..._darwin_arm64.tar.gz`
- `claude-switch_<version>_windows_amd64.zip` (no Windows arm64 build)
- `checksums.txt` — SHA-256 of every archive

Pick the archive matching your OS and CPU, verify the checksum, extract the `claude-switch` binary, and put it somewhere on `PATH`.

### macOS — Homebrew (recommended, Intel + Apple Silicon)

```bash
brew install jleal52/tap/claude-switch
```

One command, both architectures: the formula publishes both `darwin_amd64` and `darwin_arm64` URLs and Homebrew picks the matching one automatically. Verify after install:

```bash
claude-switch pair          # → "pair requires a server base URL" (binary OK)
which claude-switch         # → /opt/homebrew/bin/claude-switch  (Apple Silicon)
                            #   /usr/local/bin/claude-switch     (Intel)
```

After `brew install` you still need to pair with the server and start the wrapper. Pairing only writes credentials; the wrapper has to be running for the portal to see it (otherwise the portal shows `wrapper offline`).

```bash
claude-switch pair https://your-server.example.com   # opens device-code flow
brew services start claude-switch                    # run as a per-user LaunchAgent
```

`brew services start` (without `sudo`) registers the wrapper as a **per-user LaunchAgent** in `~/Library/LaunchAgents/`, so it runs under your uid and can read your `~/.claude` transcripts and stored credentials. Logs land in `$(brew --prefix)/var/log/claude-switch.log`. Manage it with:

```bash
brew services list                          # see status
brew services restart claude-switch         # after upgrade or config change
brew services stop claude-switch            # unload the agent
```

Day-to-day:

```bash
brew upgrade claude-switch  # pull the latest tagged release
brew services restart claude-switch  # pick up the new binary
brew uninstall claude-switch
brew reinstall claude-switch
```

If you'd rather tap once and reference the formula by short name:

```bash
brew tap jleal52/tap        # clones github.com/jleal52/homebrew-tap
brew install claude-switch
```

**No Gatekeeper workaround needed.** Unlike the manual tarball below, Homebrew downloads via its own client (not Safari/curl) so the binary is never quarantined. The binaries are still **not code-signed or notarized**, so `spctl --assess` will report them as unsigned — that's expected and doesn't block execution.

> The formula is rendered by goreleaser on every tagged release and pushed to [`github.com/jleal52/homebrew-tap`](https://github.com/jleal52/homebrew-tap). Available since `v0.3.1`.

### macOS — manual tarball (fallback)

```bash
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
VERSION=$(curl -fsSL https://api.github.com/repos/jleal52/claude-switch/releases/latest | sed -nE 's/.*"tag_name": *"v([^"]+)".*/\1/p')
URL="https://github.com/jleal52/claude-switch/releases/download/v${VERSION}/claude-switch_${VERSION}_darwin_${ARCH}.tar.gz"

curl -fLO "$URL"
curl -fLO "https://github.com/jleal52/claude-switch/releases/download/v${VERSION}/checksums.txt"
shasum -a 256 -c checksums.txt --ignore-missing

tar -xzf "claude-switch_${VERSION}_darwin_${ARCH}.tar.gz"
sudo install -m 0755 claude-switch /usr/local/bin/claude-switch

# Binaries are not notarized — Gatekeeper will quarantine the download:
xattr -d com.apple.quarantine /usr/local/bin/claude-switch 2>/dev/null || true

claude-switch --help
```

### Linux — `.deb` (Debian / Ubuntu, recommended)

```bash
ARCH=$(dpkg --print-architecture)   # → amd64 or arm64
VERSION=$(curl -fsSL https://api.github.com/repos/jleal52/claude-switch/releases/latest | sed -nE 's/.*"tag_name": *"v([^"]+)".*/\1/p')
curl -fLO "https://github.com/jleal52/claude-switch/releases/download/v${VERSION}/claude-switch_${VERSION}_linux_${ARCH}.deb"
sudo apt install "./claude-switch_${VERSION}_linux_${ARCH}.deb"
```

### Linux — `.rpm` (Fedora / RHEL / openSUSE)

```bash
ARCH=$(uname -m)                    # → x86_64 or aarch64
VERSION=$(curl -fsSL https://api.github.com/repos/jleal52/claude-switch/releases/latest | sed -nE 's/.*"tag_name": *"v([^"]+)".*/\1/p')
curl -fLO "https://github.com/jleal52/claude-switch/releases/download/v${VERSION}/claude-switch_${VERSION}_linux_${ARCH}.rpm"
sudo dnf install "./claude-switch_${VERSION}_linux_${ARCH}.rpm"
```

Both packages drop the binary at `/usr/bin/claude-switch` and install a **systemd user unit** at `/usr/lib/systemd/user/claude-switch.service`. After installing, pair the wrapper and start the service as your user (no `sudo`):

```bash
claude-switch pair https://your-server.example.com
systemctl --user enable --now claude-switch
systemctl --user status claude-switch        # verify it's running
journalctl --user -u claude-switch -f        # tail logs
```

If you want the wrapper to keep running while you're logged out, enable user lingering once:

```bash
sudo loginctl enable-linger "$USER"
```

> No hosted APT / YUM repo yet — `apt upgrade` won't pull new versions automatically. Re-run the `curl` + `apt install` (or `dnf install`) on each release. A signed `gh-pages` repo is on the roadmap.

### Linux — manual tarball (any distro, no service unit)

```bash
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
VERSION=$(curl -fsSL https://api.github.com/repos/jleal52/claude-switch/releases/latest | sed -nE 's/.*"tag_name": *"v([^"]+)".*/\1/p')
URL="https://github.com/jleal52/claude-switch/releases/download/v${VERSION}/claude-switch_${VERSION}_linux_${ARCH}.tar.gz"

curl -fLO "$URL"
curl -fLO "https://github.com/jleal52/claude-switch/releases/download/v${VERSION}/checksums.txt"
sha256sum -c checksums.txt --ignore-missing

tar -xzf "claude-switch_${VERSION}_linux_${ARCH}.tar.gz"
sudo install -m 0755 claude-switch /usr/local/bin/claude-switch

claude-switch --help
```

### Windows (PowerShell)

Only `windows_amd64` is published.

```powershell
$Version = (Invoke-RestMethod 'https://api.github.com/repos/jleal52/claude-switch/releases/latest').tag_name -replace '^v',''
$Url     = "https://github.com/jleal52/claude-switch/releases/download/v$Version/claude-switch_${Version}_windows_amd64.zip"
$Dest    = "$Env:LOCALAPPDATA\claude-switch"

New-Item -ItemType Directory -Force -Path $Dest | Out-Null
Invoke-WebRequest -Uri $Url -OutFile "$Dest\claude-switch.zip"
Invoke-WebRequest -Uri "https://github.com/jleal52/claude-switch/releases/download/v$Version/checksums.txt" `
                  -OutFile "$Dest\checksums.txt"

# Verify
$expected = (Select-String -Path "$Dest\checksums.txt" -Pattern "windows_amd64.zip").Line.Split(' ')[0]
$actual   = (Get-FileHash "$Dest\claude-switch.zip" -Algorithm SHA256).Hash.ToLower()
if ($expected -ne $actual) { throw "checksum mismatch" }

Expand-Archive -Force "$Dest\claude-switch.zip" -DestinationPath $Dest

# Add to PATH for the current user (new terminals will see it):
[Environment]::SetEnvironmentVariable(
  "Path", "$([Environment]::GetEnvironmentVariable('Path','User'));$Dest", "User")

claude-switch --help
```

The wrapper relies on Windows ConPTY (built into Windows 10 1809+ and Windows 11).

### Build from source (any platform)

Requires Go 1.24+:

```bash
git clone https://github.com/jleal52/claude-switch.git
cd claude-switch
make build               # → bin/claude-switch
sudo install -m 0755 bin/claude-switch /usr/local/bin/claude-switch    # macOS/Linux
# Windows: copy bin\claude-switch.exe to a directory on %PATH%
```

### After installing — pair with a server

The wrapper needs `claude` (the upstream CLI) on `PATH` and a one-time pairing with a `claude-switch-server`:

```bash
claude-switch pair https://your-claude-switch-server.example.com
# follow the device-code flow shown in the terminal
claude-switch          # run normally from then on
```

Credentials are written to a per-user file (location reported by `claude-switch pair`); delete that file or call the server's revoke endpoint to unpair.

---

## Roadmap for distribution

- **macOS Homebrew tap** — ✅ live since `v0.3.1` (`.goreleaser.yaml` `brews:` block, pushes `Formula/claude-switch.rb` to [`jleal52/homebrew-tap`](https://github.com/jleal52/homebrew-tap) on every tagged release).
- **Scoop bucket** (`scoops:`) for `scoop install claude-switch` on Windows.
- **`winget` manifest** (separate publishing step) for `winget install claude-switch`.
- **`nfpms:`** to emit `.deb` / `.rpm` / `.apk` packages alongside the Linux tarballs.
- **Code signing + notarization** for macOS so `spctl --assess` accepts the binary without warnings (requires an Apple Developer ID + `notarize:` block in goreleaser).
- **Docker image for the wrapper** — currently only the *server* has a `Dockerfile.server`; the wrapper has no image because it must spawn a host PTY.

### How a release is cut

```bash
git tag v0.X.Y
git push origin v0.X.Y
```

That triggers `.github/workflows/release.yml` → goreleaser → publishes the GitHub Release **and** updates the Homebrew formula in the tap. End users see the new version with `brew upgrade claude-switch`.

The release pipeline depends on the `HOMEBREW_TAP_TOKEN` repository secret (fine-grained PAT with `Contents: write` on `jleal52/homebrew-tap`); rotate it before it expires or releases will fail at the brew step.

PRs welcome.
