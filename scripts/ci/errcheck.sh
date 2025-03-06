#!/bin/sh

if [ -z ${PKG+x} ]; then echo "PKG is not set"; exit 1; fi
if [ -z ${ROOT_DIR+x} ]; then echo "ROOT_DIR is not set"; exit 1; fi

echo ${PATH_TO_GO_BINARY}
echo "\n"
echo "errcheck:"
OUT=$(${PATH_TO_GO_BINARY}/errcheck $PKG/... 2>&1 | grep --invert-match -E "(/example)")
if [ -n "$OUT" ]; then echo "$OUT"; PROBLEM=1; fi

if [ -n "$PROBLEM" ]; then exit 1; fi
