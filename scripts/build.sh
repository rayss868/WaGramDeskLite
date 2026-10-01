#!/usr/bin/env bash
# Build WaGramDeskLite: compile Windows resources, build the executable and the
# Inno Setup installer into dist/.

set -euo pipefail
cd "$(dirname "$0")/.."

# github.com/tc-hib/go-winres (pure Go resource compiler, no windres needed)
if command -v go-winres >/dev/null 2>&1; then
    RESTOOL="go-winres"
elif [ -x "$(go env GOPATH)/bin/go-winres.exe" ]; then
    RESTOOL="$(go env GOPATH)/bin/go-winres.exe"
else
    echo "go-winres not found. Install it with: go install github.com/tc-hib/go-winres@latest" >&2
    exit 1
fi
OUT="dist/WaGramDeskLite.exe"

echo "[1/4] Compiling Windows resources (icon, manifest, VERSIONINFO)..."
cd build
"$RESTOOL" make -arch amd64 --in winres.json
cp rsrc_windows_amd64.syso ../cmd/wagramdesklite/rsrc.syso
cd ..

echo "[2/4] Building $OUT..."
mkdir -p dist
go build -ldflags="-H windowsgui -s -w" -o "$OUT" ./cmd/wagramdesklite
# The tray icon is loaded at runtime from icon.ico next to the executable.
cp assets/icon.ico icon.ico
cp assets/icon.ico dist/icon.ico

# Inno Setup command-line compiler. The installer is optional: the executable
# is usable on its own, so a missing ISCC warns and skips it rather than failing.
if command -v iscc >/dev/null 2>&1; then
    ISCC="iscc"
elif [ -x "/c/Program Files (x86)/Inno Setup 6/ISCC.exe" ]; then
    ISCC="/c/Program Files (x86)/Inno Setup 6/ISCC.exe"
else
    ISCC=""
fi

if [ -n "$ISCC" ]; then
    echo "[3/4] Compiling the installer (dist/WaGramDeskLiteSetup.exe)..."
    # ISCC flags start with '/', which git-bash would rewrite into Windows paths,
    # so disable MSYS argument conversion for this one call.
    MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL="*" "$ISCC" /O"dist" /F"WaGramDeskLiteSetup" scripts/WaGramDeskLiteSetup.iss >/dev/null
    echo "[4/4] Done: $OUT and dist/WaGramDeskLiteSetup.exe"
else
    echo "[3/4] Skipped the installer: Inno Setup (ISCC.exe) not found." >&2
    echo "      Install it from https://jrsoftware.org/isdl.php to also build dist/WaGramDeskLiteSetup.exe" >&2
    echo "[4/4] Done: $OUT (no installer)"
fi