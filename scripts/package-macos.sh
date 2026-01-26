#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_NAME="Nexus"
APP_BUNDLE_NAME="${APP_NAME}.app"
BUNDLE_ID="${BUNDLE_ID:-com.haasonsaas.nexus.mac}"
OUTPUT_DIR="${OUTPUT_DIR:-${ROOT_DIR}/dist/macos}"
SWIFT_PACKAGE_DIR="${ROOT_DIR}/apps/macos"
INFO_TEMPLATE="${SWIFT_PACKAGE_DIR}/Packaging/Info.plist.template"
SIGN_IDENTITY="${SIGN_IDENTITY:--}"
NOTARIZE_PROFILE="${NOTARIZE_PROFILE:-}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() {
    echo -e "${GREEN}==>${NC} $1"
}

warn() {
    echo -e "${YELLOW}==>${NC} $1"
}

fail() {
    echo -e "${RED}Error:${NC} $1" >&2
    exit 1
}

require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        fail "Required command not found: $1"
    fi
}

require_command git
require_command swift
require_command go
require_command lipo
require_command hdiutil
require_command codesign
require_command ditto

if [ ! -f "$INFO_TEMPLATE" ]; then
    fail "Info.plist template not found at $INFO_TEMPLATE"
fi

VERSION_RAW="$(git -C "$ROOT_DIR" describe --tags --always --dirty)"
VERSION_SHORT="$(echo "$VERSION_RAW" | sed -E 's/^v//' | cut -d- -f1)"
VERSION_BUILD="$(echo "$VERSION_RAW" | sed -E 's/^v//')"
VERSION_SAFE="$(echo "$VERSION_BUILD" | tr '/' '_' | tr ' ' '_')"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

log "Building Swift binaries"
SWIFT_ARM_BIN_PATH="$(swift build --package-path "$SWIFT_PACKAGE_DIR" -c release --arch arm64 --show-bin-path)"
SWIFT_X86_BIN_PATH="$(swift build --package-path "$SWIFT_PACKAGE_DIR" -c release --arch x86_64 --show-bin-path)"

SWIFT_ARM_BIN="${SWIFT_ARM_BIN_PATH}/NexusMac"
SWIFT_X86_BIN="${SWIFT_X86_BIN_PATH}/NexusMac"

if [ ! -f "$SWIFT_ARM_BIN" ]; then
    fail "Swift arm64 binary not found at $SWIFT_ARM_BIN"
fi

if [ ! -f "$SWIFT_X86_BIN" ]; then
    fail "Swift x86_64 binary not found at $SWIFT_X86_BIN"
fi

UNIVERSAL_SWIFT_BIN="${TMP_DIR}/NexusMac"
lipo -create -output "$UNIVERSAL_SWIFT_BIN" "$SWIFT_ARM_BIN" "$SWIFT_X86_BIN"

log "Building Go binaries"
GO_OUTPUT_DIR="${TMP_DIR}/go"
mkdir -p "$GO_OUTPUT_DIR"

build_go_binary() {
    local name=$1
    local pkg=$2
    local arch=$3
    local output="${GO_OUTPUT_DIR}/${name}-${arch}"
    env CGO_ENABLED=0 GOOS=darwin GOARCH="${arch}" \
        go build -ldflags="-s -w" -o "$output" "$pkg"
}

pushd "$ROOT_DIR" >/dev/null
build_go_binary "nexus" "./cmd/nexus" "arm64"
build_go_binary "nexus" "./cmd/nexus" "amd64"
build_go_binary "nexus-edge" "./cmd/nexus-edge" "arm64"
build_go_binary "nexus-edge" "./cmd/nexus-edge" "amd64"
popd >/dev/null

UNIVERSAL_NEXUS_BIN="${GO_OUTPUT_DIR}/nexus"
UNIVERSAL_EDGE_BIN="${GO_OUTPUT_DIR}/nexus-edge"

lipo -create -output "$UNIVERSAL_NEXUS_BIN" "${GO_OUTPUT_DIR}/nexus-arm64" "${GO_OUTPUT_DIR}/nexus-amd64"
lipo -create -output "$UNIVERSAL_EDGE_BIN" "${GO_OUTPUT_DIR}/nexus-edge-arm64" "${GO_OUTPUT_DIR}/nexus-edge-amd64"

log "Preparing app bundle"
mkdir -p "$OUTPUT_DIR"

APP_DIR="${OUTPUT_DIR}/${APP_BUNDLE_NAME}"
CONTENTS_DIR="${APP_DIR}/Contents"
MACOS_DIR="${CONTENTS_DIR}/MacOS"
RESOURCES_DIR="${CONTENTS_DIR}/Resources"

rm -rf "$APP_DIR"
mkdir -p "$MACOS_DIR" "$RESOURCES_DIR"

cp "$UNIVERSAL_SWIFT_BIN" "${MACOS_DIR}/NexusMac"
cp "$UNIVERSAL_NEXUS_BIN" "${RESOURCES_DIR}/nexus"
cp "$UNIVERSAL_EDGE_BIN" "${RESOURCES_DIR}/nexus-edge"

chmod 755 "${MACOS_DIR}/NexusMac" "${RESOURCES_DIR}/nexus" "${RESOURCES_DIR}/nexus-edge"

sed -e "s|__BUNDLE_ID__|${BUNDLE_ID}|g" \
    -e "s|__VERSION_SHORT__|${VERSION_SHORT}|g" \
    -e "s|__VERSION_BUILD__|${VERSION_BUILD}|g" \
    "$INFO_TEMPLATE" > "${CONTENTS_DIR}/Info.plist"

if [ "$SIGN_IDENTITY" = "-" ]; then
    log "Ad-hoc signing app bundle"
    codesign --force --deep --sign - "$APP_DIR"
else
    log "Signing app bundle with identity ${SIGN_IDENTITY}"
    codesign --force --deep --options runtime --timestamp --sign "$SIGN_IDENTITY" "$APP_DIR"
fi

codesign --verify --deep --strict "$APP_DIR" >/dev/null 2>&1 || warn "codesign verification failed"

ARTIFACT_BASE="nexus-macos-universal-${VERSION_SAFE}"
ZIP_PATH="${OUTPUT_DIR}/${ARTIFACT_BASE}.zip"
DMG_PATH="${OUTPUT_DIR}/${ARTIFACT_BASE}.dmg"

rm -f "$ZIP_PATH" "$DMG_PATH"

log "Creating ZIP archive"
ditto -c -k --sequesterRsrc --keepParent "$APP_DIR" "$ZIP_PATH"

log "Creating DMG installer"
DMG_STAGING="${TMP_DIR}/dmg"
mkdir -p "$DMG_STAGING"
cp -R "$APP_DIR" "$DMG_STAGING/"
ln -s /Applications "$DMG_STAGING/Applications"
hdiutil create -volname "$APP_NAME" -srcfolder "$DMG_STAGING" -ov -format UDZO -imagekey zlib-level=9 "$DMG_PATH" >/dev/null

if [ -n "$NOTARIZE_PROFILE" ]; then
    require_command xcrun
    log "Submitting DMG for notarization"
    xcrun notarytool submit "$DMG_PATH" --keychain-profile "$NOTARIZE_PROFILE" --wait
    log "Stapling notarization"
    xcrun stapler staple "$DMG_PATH"
    xcrun stapler staple "$APP_DIR" || true
fi

log "Artifacts ready"
echo "  App: ${APP_DIR}"
echo "  ZIP: ${ZIP_PATH}"
echo "  DMG: ${DMG_PATH}"
