#!/usr/bin/env bash
#
# build-wsecho.sh -- build WSECHO.ELF (user/go/wsecho, M212c issue #3874)
# with the GOOS=virelai fork toolchain. WSECHO is gate-only — the go-irc
# spec's ws echo leg stages it onto the share by name; it ships nowhere
# in image/apps.txt.
#
# Usage: bash tools/go/build-wsecho.sh
#
# Thin wrapper over build-web.sh (the generic dir->ELF builder), kept so
# ensure-guest-elf.sh's name->build-<lower>.sh convention resolves.

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
exec bash "$REPO/tools/go/build-web.sh" wsecho WSECHO "$@"
