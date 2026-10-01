#!/usr/bin/env bash
# Extract each embedded JS const from its Go raw string and syntax-check it.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

extract() {
  # $1 = file, $2 = const name
  python - "$1" "$2" <<'PY'
import re, sys
path, name = sys.argv[1], sys.argv[2]
src = open(path, encoding='utf-8', errors='replace').read()
m = re.search(r'const\s+' + re.escape(name) + r'\s*=\s*`(.*?)`', src, re.S)
if not m:
    print('NOT FOUND: %s in %s' % (name, path), file=sys.stderr)
    sys.exit(2)
# The scripts carry non-ASCII glyphs; Windows defaults stdout to cp1252.
sys.stdout.buffer.write(m.group(1).encode('utf-8'))
PY
}

fail=0
for pair in "internal/app/notification_windows.go:notificationPolyfillJS" \
            "internal/app/overlay_windows.go:accountOverlayScript" \
            "internal/app/privacy_windows.go:privacyBlurScriptTemplate"; do
  f="${pair%%:*}"; c="${pair##*:}"
  out="/tmp/$c.js"
  extract "$f" "$c" > "$out" || { echo "extract failed: $c"; fail=1; continue; }
  if node --check "$out" 2>/tmp/err.txt; then
    echo "OK   syntax: $c ($(wc -c < "$out") bytes)"
  else
    echo "FAIL syntax: $c"
    cat /tmp/err.txt
    fail=1
  fi
done
exit $fail
