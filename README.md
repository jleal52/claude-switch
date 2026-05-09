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

That command works on both Intel and Apple Silicon — the tap formula picks the matching `darwin_amd64` / `darwin_arm64` archive automatically. To upgrade later:

```bash
brew upgrade claude-switch
```

If you prefer not to pin the tap each time, you can tap it once:

```bash
brew tap jleal52/tap        # adds github.com/jleal52/homebrew-tap
brew install claude-switch
```

> The tap repo is `github.com/jleal52/homebrew-tap`; goreleaser pushes a fresh formula to it on every release.

### macOS — manual tarball (fallback)

```bash
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
VERSION=<paste latest tag, e.g. 0.1.0>
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

### Linux

```bash
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
VERSION=<paste latest tag>
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
$Version = "<paste latest tag>"
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

- **macOS Homebrew tap** — done (`.goreleaser.yaml` `brews:` block; pushes to `jleal52/homebrew-tap` on each release).
- **Scoop bucket** (`scoops:`) for `scoop install claude-switch` on Windows.
- **`winget` manifest** (separate publishing step) for `winget install claude-switch`.
- **`nfpms:`** to emit `.deb` / `.rpm` / `.apk` packages alongside the Linux tarballs.
- **Docker image for the wrapper** — currently only the *server* has a `Dockerfile.server`; the wrapper has no image because it must spawn a host PTY.

### One-time setup needed before the next release

Goreleaser will fail until the tap is reachable and authorized:

1. Create an empty public repo `github.com/jleal52/homebrew-tap` (must be exactly that name — Homebrew convention).
2. Generate a fine-grained PAT with `Contents: write` on that single repo: <https://github.com/settings/tokens?type=beta>.
3. Add it as the `HOMEBREW_TAP_TOKEN` Actions secret in `github.com/jleal52/claude-switch` (Settings → Secrets and variables → Actions).

After that, `git tag v0.1.0 && git push --tags` triggers `.github/workflows/release.yml`, which runs goreleaser and pushes `Formula/claude-switch.rb` into the tap. Users can then `brew install jleal52/tap/claude-switch` immediately.

PRs welcome.
