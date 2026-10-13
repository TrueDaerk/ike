#!/usr/bin/env bash
# Shell golden fixture of the complexity engine (#2915).
#
# Every function exercises one construct family; expected values live in
# fixture.golden.json. Neither lizard nor the cognitive tools read shell, so
# the values are hand-derived from the rules the Go and Python fixtures
# verify against their reference tools (see golden_test.go).

else_if_chain() {
  if [ "$1" = 1 ]; then
    echo one
  elif [ "$1" = 2 ]; then
    echo two
  elif [ "$1" = 3 ]; then
    echo three
  else
    # comment-only lines do not count toward NLOC
    if [ "$1" -lt 0 ]; then
      echo negative
    fi
  fi
}

case_arms() {
  case "$1" in
    1)
      if [ -n "$2" ]; then
        echo one
      fi
      ;;
    2 | 3)
      echo two
      ;;
    *)
      if [ -z "$2" ]; then
        echo other
      fi
      ;;
  esac
}

nested_loops() {
  local n=0
  for x in "$@"; do
    for ((j = 0; j < x; j++)); do
      while [ "$n" -lt 100 ]; do
        if [ "$j" -eq 2 ]; then
          continue 2
        fi
        n=$((n + 1))
      done
    done
  done
  until [ "$n" -le 50 ]; do
    n=$((n - 1))
  done
  echo "$n"
}

boolean_sequences() {
  if [ -n "$1" ] && [ -n "$2" ] && [ -n "$3" ]; then
    return 1
  fi
  if [ -n "$1" ] && [ -n "$2" ] || [ -n "$3" ]; then
    return 2
  fi
  [ -f "$1" ] || [ -d "$1" ] || return 3
  return 0
}

nested_functions() {
  helper() {
    if [ -n "$1" ]; then
      echo "$1"
    fi
  }
  helper "$1"
}

countdown() {
  if [ "$1" -le 0 ]; then
    return 0
  fi
  echo "$1"
  countdown $(($1 - 1))
}

select_loop() {
  select opt in a b c; do
    case "$opt" in
      a) break ;;
      *) echo "$opt" ;;
    esac
  done
}

plain() {
  :
}

# documented carries a comment above its header; it lies outside the unit,
# so the comment density stays 0 (#2934).
documented() {
  echo "$1"
}

comment_heavy() {
  # first explain
  # then explain more
  local y=$1
  # and again
  echo "$y"
}
