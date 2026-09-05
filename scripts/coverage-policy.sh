#!/usr/bin/env bash
#
# coverage-policy.sh — enforce the per-package ≥90% coverage policy advisorily.
#
# Reads .coverage-policy.yaml (the machine-readable form of the rule) and the
# coverage profile the test run already produced. A package is FLAGGED when it is below the
# threshold AND is neither in the `excluded` list nor matched by a `not_counted`
# prefix.
#
# It exits non-zero when there are violations so the wrapping CI job surfaces
# them; that job is `allow_failure: true`, so this is ADVISORY — it never blocks
# an MR. The remedy for a flagged package is one of:
#   1. add tests to reach the threshold, or
#   2. add it to .coverage-policy.yaml `excluded:` with a one-line rationale.
#
# Usage:
#   scripts/coverage-policy.sh                  # uses .coverage-policy.yaml
#   scripts/coverage-policy.sh path/to/policy.yaml
#
set -uo pipefail

MODULE="gitlab.com/phpboyscout/afmpeg"
POLICY="${1:-.coverage-policy.yaml}"

if [ ! -f "$POLICY" ]; then
	echo "coverage-policy: policy file not found: $POLICY" >&2
	exit 2
fi

threshold=$(awk -F': *' '/^threshold:/ {print $2; exit}' "$POLICY")
[ -z "$threshold" ] && threshold=90

# Parse the `not_counted:` YAML list (items: "  - prefix").
mapfile -t not_counted < <(
	awk '
		/^not_counted:/ {f=1; next}
		/^[A-Za-z_]+:/  {f=0}
		f && /^[[:space:]]*-[[:space:]]*/ {
			sub(/^[[:space:]]*-[[:space:]]*/, "");
			sub(/[[:space:]]*(#.*)?$/, "");
			if (length($0)) print
		}
	' "$POLICY"
)

# Parse the `excluded:` package keys ("  - { pkg: X, reason: ... }").
mapfile -t excluded < <(
	grep -oE 'pkg:[[:space:]]*[^,}]+' "$POLICY" | sed -E 's/pkg:[[:space:]]*//; s/[[:space:]]+$//'
)

echo "coverage-policy: threshold ${threshold}%, ${#excluded[@]} excluded package(s), ${#not_counted[@]} not-counted prefix(es)"

is_not_counted() {
	local rel="$1" nc
	for nc in "${not_counted[@]}"; do
		case "$nc" in
			*/) case "$rel/" in "$nc"*) return 0 ;; esac ;;
			*)  [ "$rel" = "$nc" ] && return 0 ;;
		esac
	done
	return 1
}

is_excluded() {
	local rel="$1" ex
	for ex in "${excluded[@]}"; do
		[ "$rel" = "$ex" ] && return 0
	done
	return 1
}

COVER="${COVER_PROFILE:-cover.out}"

# The profile is read, not regenerated. go-test has already run
# `go test -race -coverprofile=cover.out ./...` and publishes it, so running the
# suite again here cost a second full run of the module for a number that already
# existed (60 runs, 229 runner minutes across afmpeg and go-tool-base over the 21
# days to 2026-09-03, at a 177 s median).
#
# Every way this can measure nothing still fails loudly. Discarding stderr is
# what let this script report OK 12ms into a run that takes minutes: `go test`
# failed, the reason went to /dev/null, grep matched nothing, and a loop over
# nothing found no violations and exited 0. An advisory job that cannot fail is
# worse than no job, because it reads as a green gate. An artifact can be absent,
# empty, or another module's, which is the same lie by a newer route.
if [ ! -f "$COVER" ]; then
	if [ -n "${CI:-}" ]; then
		echo "coverage-policy: ${COVER} is missing; it comes from the go-test job's artifact (needs: go-test, artifacts: true)." >&2
		echo "coverage-policy: nothing was measured." >&2
		exit 2
	fi
	echo "coverage-policy: ${COVER} not found, generating it (this can take a few minutes)"
	raw=$(go test -race -coverprofile="$COVER" ./... 2>&1)
	status=$?
	if [ "$status" -ne 0 ]; then
		printf '%s\n' "$raw" >&2
		echo "coverage-policy: \`go test\` exited ${status}; nothing was measured." >&2
		exit 2
	fi
fi

if [ ! -s "$COVER" ]; then
	echo "coverage-policy: ${COVER} is empty, so there was nothing to check." >&2
	exit 2
fi

if ! grep -q "^${MODULE}/" "$COVER"; then
	echo "coverage-policy: ${COVER} carries no line for ${MODULE}; it is empty of this module or belongs to another one." >&2
	exit 2
fi

echo "coverage-policy: reading ${COVER} ($(grep -c . "$COVER") profile lines)"

# Aggregate per package the way `go test -cover` does: covered statements over
# total statements, a block counted once however many times it ran. Emitted in
# `go test` output shape, so the loop below is unchanged.
cover_out=$(awk '
	/^mode:/ { next }
	{
		split($1, a, ":")
		file = a[1]
		idx = match(file, /\/[^\/]*$/)
		if (idx == 0) next
		pkg = substr(file, 1, idx - 1)
		total[pkg] += $2
		if ($3 + 0 > 0) covered[pkg] += $2
	}
	END {
		for (p in total)
			if (total[p] > 0)
				printf "ok  \t%s\tcoverage: %.1f%% of statements\n", p, 100 * covered[p] / total[p]
	}
' "$COVER")

if [ -z "$cover_out" ]; then
	echo "coverage-policy: ${COVER} parsed to no package results, so there was nothing to check." >&2
	exit 2
fi

measured=0
violations=0
while IFS= read -r line; do
	[ -z "$line" ] && continue
	pkg=$(printf '%s\n' "$line" | grep -oE "${MODULE}/[^[:space:]]+" | head -1)
	[ -z "$pkg" ] && continue
	rel=${pkg#"${MODULE}/"}
	pct=$(printf '%s\n' "$line" | grep -oE 'coverage: [0-9.]+%' | grep -oE '[0-9.]+')
	[ -z "$pct" ] && continue
	measured=$((measured + 1))

	# Above threshold → fine.
	if awk "BEGIN{exit !($pct >= $threshold)}"; then
		continue
	fi
	# Below threshold but allowed → fine.
	is_not_counted "$rel" && continue
	is_excluded "$rel" && continue

	printf 'VIOLATION: %s at %s%% (< %s%%) is not on the coverage exclusion list (.coverage-policy.yaml)\n' "$rel" "$pct" "$threshold"
	violations=$((violations + 1))
done <<< "$cover_out"

echo ""
# The last way to pass without checking: every line skipped for its own reason.
if [ "$measured" -eq 0 ]; then
	echo "coverage-policy: parsed no package coverage out of ${COVER}, so this run checked nothing." >&2
	exit 2
fi

if [ "$violations" -gt 0 ]; then
	echo "coverage-policy: ${violations} package(s) below ${threshold}% and not excluded."
	echo "  Fix: add tests to reach ${threshold}%, OR add the package to .coverage-policy.yaml 'excluded:' with a rationale."
	exit 1
fi

echo "coverage-policy: OK — ${measured} package(s) measured, every countable one ≥ ${threshold}% or explicitly excluded."
exit 0
