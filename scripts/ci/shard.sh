#!/usr/bin/env sh
# shard.sh PKG INDEX TOTAL — print a `go test -run` pattern that selects every
# TOTAL-th top-level test of PKG, starting at the INDEX-th (1-based), so TOTAL
# runners can split one package's tests between them (#3054):
#
#   go test ./internal/app -run "$(scripts/ci/shard.sh ./internal/app 2 3)"
#
# The split is by position in `go test -list` order, which is source order, so
# a shard's membership moves only when tests are added or removed. Subtests
# follow their parent. The pattern is anchored, so a shard never runs a test
# whose name merely contains another's.
set -eu

if [ $# -ne 3 ]; then
	echo "usage: $0 PKG INDEX TOTAL" >&2
	exit 2
fi
pkg=$1
idx=$2
total=$3
if [ "$idx" -lt 1 ] || [ "$idx" -gt "$total" ]; then
	echo "shard.sh: INDEX must be between 1 and TOTAL" >&2
	exit 2
fi

pattern=$(go test -list '^Test' "$pkg" | awk -v n="$total" -v i="$idx" '
	/^Test/ {
		c++
		if (c % n == i % n) {
			printf "%s%s", sep, $0
			sep = "|"
		}
	}')
if [ -z "$pattern" ]; then
	echo "shard.sh: no tests selected for shard $idx of $total in $pkg" >&2
	exit 1
fi
printf '^(%s)$\n' "$pattern"
