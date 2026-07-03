#!/usr/bin/env bash
#
# package-app.sh — assemble a macOS git-repo-tracker.app bundle around a
# prebuilt binary. GoReleaser OSS cannot build .app bundles (that is a Pro-only
# feature), so we wrap the binary ourselves. The bundle's Info.plist sets
# LSUIElement, so the app runs as a menu-bar agent: tray icon, no Dock icon,
# no main window.
#
# Usage:
#   build/macos/package-app.sh --binary <path> --version <ver> --out <dir> [--zip <zipfile>]
#
# Produces <dir>/git-repo-tracker.app, and (with --zip) a ditto zip of it that
# preserves symlinks/permissions so Homebrew's cask can install it verbatim.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_NAME="git-repo-tracker"

binary=""
version=""
out=""
zipfile=""

while [[ $# -gt 0 ]]; do
	case "$1" in
	--binary | --version | --out | --zip)
		# Reject a missing value or one that looks like the next flag, so a typo
		# fails clearly instead of silently consuming the following option.
		if [[ $# -lt 2 || "${2:-}" == --* ]]; then
			echo "package-app.sh: $1 requires a value" >&2
			exit 2
		fi
		case "$1" in
		--binary) binary="$2" ;;
		--version) version="$2" ;;
		--out) out="$2" ;;
		--zip) zipfile="$2" ;;
		esac
		shift 2
		;;
	*) echo "package-app.sh: unknown argument: $1" >&2; exit 2 ;;
	esac
done

for req in binary version out; do
	if [[ -z "${!req}" ]]; then
		echo "package-app.sh: missing required --$req" >&2
		exit 2
	fi
done
if [[ ! -f "$binary" ]]; then
	echo "package-app.sh: binary not found: $binary" >&2
	exit 1
fi

app="$out/$APP_NAME.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

install -m 0755 "$binary" "$app/Contents/MacOS/$APP_NAME"
install -m 0644 "$SCRIPT_DIR/icon.icns" "$app/Contents/Resources/icon.icns"

# Substitute the version into the Info.plist template.
sed "s/__VERSION__/${version}/g" "$SCRIPT_DIR/Info.plist" >"$app/Contents/Info.plist"

echo "package-app.sh: built $app (version $version)"

if [[ -n "$zipfile" ]]; then
	# ditto produces a macOS-native zip that preserves the bundle structure.
	rm -f "$zipfile"
	ditto -c -k --sequesterRsrc --keepParent "$app" "$zipfile"
	echo "package-app.sh: zipped -> $zipfile"
fi
