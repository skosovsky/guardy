#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
    echo 'Usage: make release-patch OR make release-break' >&2
    exit 1
fi

exec python3 scripts/release.py release "$1" --modules "$2"
