#!/bin/sh
# Emit the build version string for LDFLAGS injection (main.Version).
#
# Project versioning convention (see docs/18-runbook.md "Versioning"):
#   1. git describe --tags --dirty   → once annotated tags exist:
#        v1.2.0            (exactly on a tag)
#        v1.2.0-4-gabc1234 (4 commits past v1.2.0)
#        ...-dirty         (uncommitted changes present)
#   2. Pre-first-tag fallback: 0.0.0-<commit-count>-g<sha>[-dirty]
#        semver-ish and monotonic (commit count only grows), so dev/nightly
#        builds stay distinguishable instead of all reading "0.0.0-dev".
#   3. Non-git fallback (tarball/CI without .git): 0.0.0-unknown
#
# Tag a release:  git tag -a v1.2.0 -m "v1.2.0" && git push --tags
#
# This script is the copy-paste unit for standardizing versioning across the
# sibling Go projects; keep it dependency-free (POSIX sh + git only).
set -eu

# 1. Tagged path.
if desc=$(git describe --tags --dirty 2>/dev/null); then
	printf '%s\n' "$desc"
	exit 0
fi

# 2. Pre-tag fallback (no tags yet, but a git repo).
if count=$(git rev-list --count HEAD 2>/dev/null); then
	sha=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)
	dirty=""
	if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
		dirty="-dirty"
	fi
	printf '0.0.0-%s-g%s%s\n' "$count" "$sha" "$dirty"
	exit 0
fi

# 3. No git at all.
printf '0.0.0-unknown\n'
