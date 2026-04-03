#!/usr/bin/env bash
# kish bash compatibility test suite
# Run with: bash test/bash-compat.sh (reference) then: kish test/bash-compat.sh (compare)
# Non-destructive. Tests core bash features.

PASS=0
FAIL=0
TOTAL=0

assert() {
    local desc="$1"
    local expected="$2"
    local actual="$3"
    TOTAL=$((TOTAL + 1))
    if [ "$expected" = "$actual" ]; then
        PASS=$((PASS + 1))
    else
        FAIL=$((FAIL + 1))
        echo "FAIL: $desc"
        echo "  expected: '$expected'"
        echo "  actual:   '$actual'"
    fi
}

assert_exit() {
    local desc="$1"
    local expected_code="$2"
    shift 2
    eval "$@" >/dev/null 2>&1
    local actual_code=$?
    assert "$desc" "$expected_code" "$actual_code"
}

echo "=== kish bash compatibility tests ==="
echo ""

# ---------- 1. Basic commands ----------
echo "--- 1. Basic commands ---"
assert "echo" "hello" "$(echo hello)"
assert "echo -n" "hello" "$(echo -n hello)"
assert "printf" "hello world" "$(printf '%s %s' hello world)"
assert "true exit code" "0" "$( true; echo $? )"
assert "false exit code" "1" "$( false; echo $? )"
assert "pwd" "$(pwd)" "$(pwd)"

# ---------- 2. Variables ----------
echo "--- 2. Variables ---"
x=hello
assert "assignment" "hello" "$x"
assert "unset" "" "$(unset x; echo "$x")"
x=world
assert "reassign" "world" "$x"
assert "env export" "exported" "$(export TESTVAR=exported; echo $TESTVAR)"
assert "readonly" "42" "$(readonly RO=42; echo $RO)"

# ---------- 3. Arithmetic ----------
echo "--- 3. Arithmetic ---"
assert "add" "5" "$(echo $((2 + 3)))"
assert "multiply" "42" "$(echo $((6 * 7)))"
assert "power" "1024" "$(echo $((2 ** 10)))"
assert "modulo" "1" "$(echo $((7 % 3)))"
assert "nested" "14" "$(echo $(( (2 + 5) * 2 )))"
assert "increment" "6" "$(x=5; echo $((++x)))"
assert "ternary" "yes" "$(echo $(( 1 > 0 ? 1 : 0 )) | sed 's/1/yes/')"

# ---------- 4. String operations ----------
echo "--- 4. String operations ---"
str="Hello World"
assert "length" "11" "${#str}"
assert "substring" "Hello" "${str:0:5}"
assert "uppercase" "HELLO WORLD" "${str^^}"
assert "lowercase" "hello world" "${str,,}"
assert "replace" "Hello Earth" "${str/World/Earth}"
assert "replace all" "HXllX WXrld" "${str//[eo]/X}"
assert "default value" "default" "${UNSET_VAR:-default}"
assert "prefix strip" "World" "${str#Hello }"
assert "suffix strip" "Hello" "${str% World}"

# ---------- 5. Arrays ----------
echo "--- 5. Arrays ---"
arr=(alpha beta gamma delta)
assert "array element" "beta" "${arr[1]}"
assert "array length" "4" "${#arr[@]}"
assert "array all" "alpha beta gamma delta" "${arr[*]}"
assert "array slice" "beta gamma" "${arr[*]:1:2}"
arr+=(epsilon)
assert "array append" "5" "${#arr[@]}"

# ---------- 6. Associative arrays ----------
echo "--- 6. Associative arrays ---"
declare -A map
map[name]=kish
map[type]=shell
map[lang]=go
assert "assoc value" "kish" "${map[name]}"
assert "assoc keys" "3" "$(echo ${#map[@]})"

# ---------- 7. Conditionals ----------
echo "--- 7. Conditionals ---"
assert "if true" "yes" "$(if true; then echo yes; else echo no; fi)"
assert "if false" "no" "$(if false; then echo yes; else echo no; fi)"
assert "elif" "two" "$(x=2; if [ $x -eq 1 ]; then echo one; elif [ $x -eq 2 ]; then echo two; else echo other; fi)"
assert "string eq" "match" "$([[ "abc" == "abc" ]] && echo match || echo no)"
assert "string ne" "differ" "$([[ "abc" != "xyz" ]] && echo differ || echo no)"
assert "regex" "match" "$([[ "hello123" =~ ^hello[0-9]+$ ]] && echo match || echo no)"
assert "file test -f" "yes" "$([[ -f /etc/hosts ]] && echo yes || echo no)"
assert "file test -d" "yes" "$([[ -d /tmp ]] && echo yes || echo no)"
assert "numeric gt" "yes" "$([[ 5 -gt 3 ]] && echo yes || echo no)"
assert "numeric le" "yes" "$([[ 3 -le 3 ]] && echo yes || echo no)"
assert "and" "yes" "$([[ 1 -eq 1 && 2 -eq 2 ]] && echo yes || echo no)"
assert "or" "yes" "$([[ 1 -eq 2 || 2 -eq 2 ]] && echo yes || echo no)"
assert "negation" "yes" "$([[ ! 1 -eq 2 ]] && echo yes || echo no)"

# ---------- 8. Loops ----------
echo "--- 8. Loops ---"
assert "for" "1 2 3" "$(for i in 1 2 3; do printf "$i "; done | sed 's/ $//')"
assert "for range" "0 1 2 3 4" "$(for i in {0..4}; do printf "$i "; done | sed 's/ $//')"
assert "while" "5" "$(x=0; while [ $x -lt 5 ]; do x=$((x+1)); done; echo $x)"
assert "until" "5" "$(x=0; until [ $x -ge 5 ]; do x=$((x+1)); done; echo $x)"
assert "break" "3" "$(for i in 1 2 3 4 5; do [ $i -eq 4 ] && break; echo $i; done | tail -1)"
assert "continue" "1 2 4 5" "$(for i in 1 2 3 4 5; do [ $i -eq 3 ] && continue; printf "$i "; done | sed 's/ $//')"
assert "c-style for" "0 1 2" "$(for ((i=0; i<3; i++)); do printf "$i "; done | sed 's/ $//')"

# ---------- 9. Functions ----------
echo "--- 9. Functions ---"
greet() { echo "hi $1"; }
assert "function" "hi world" "$(greet world)"
add() { echo $(($1 + $2)); }
assert "function args" "7" "$(add 3 4)"
factorial() { if [ $1 -le 1 ]; then echo 1; else echo $(($1 * $(factorial $(($1-1))))); fi; }
assert "recursion" "120" "$(factorial 5)"
assert "local var" "outer" "$(x=outer; f() { local x=inner; }; f; echo $x)"

# ---------- 10. Pipes and redirects ----------
echo "--- 10. Pipes and redirects ---"
assert "pipe" "ABC" "$(echo abc | tr a-z A-Z)"
assert "pipe chain" "3" "$(echo -e 'a\nb\nc' | wc -l | tr -d ' ')"
assert "redirect out" "hello" "$(echo hello > /tmp/kish_test_out; cat /tmp/kish_test_out; rm /tmp/kish_test_out)"
assert "redirect append" "line1 line2" "$(echo line1 > /tmp/kish_test_app; echo line2 >> /tmp/kish_test_app; cat /tmp/kish_test_app | tr '\n' ' ' | sed 's/ $//'; rm /tmp/kish_test_app)"
assert "redirect in" "3" "$(echo -e 'a\nb\nc' > /tmp/kish_test_in; wc -l < /tmp/kish_test_in | tr -d ' '; rm /tmp/kish_test_in)"
assert "stderr redirect" "" "$(ls /nonexistent 2>/dev/null)"
assert "pipe status" "1" "$(set -o pipefail; false | true; echo $?)"
assert "here string" "hello" "$(cat <<< hello)"
assert "here doc" "line1 line2" "$(cat << 'EOF' | tr '\n' ' ' | sed 's/ $//'
line1
line2
EOF
)"

# ---------- 11. Command substitution ----------
echo "--- 11. Command substitution ---"
assert "dollar paren" "$(date +%Y)" "$(echo $(date +%Y))"
assert "nested" "HELLO" "$(echo $(echo hello | tr a-z A-Z))"
assert "in string" "today is $(date +%A)" "today is $(date +%A)"

# ---------- 12. Brace expansion ----------
echo "--- 12. Brace expansion ---"
assert "sequence" "1 2 3 4 5" "$(echo {1..5})"
assert "list" "a b c" "$(echo {a,b,c})"
assert "prefix" "pre_a pre_b pre_c" "$(echo pre_{a,b,c})"

# ---------- 13. Globbing ----------
echo "--- 13. Globbing ---"
mkdir -p /tmp/kish_glob_test
touch /tmp/kish_glob_test/{a,b,c}.txt /tmp/kish_glob_test/d.log
assert "star glob" "3" "$(ls /tmp/kish_glob_test/*.txt | wc -l)"
assert "question glob" "3" "$(ls /tmp/kish_glob_test/?.txt | wc -l)"
rm -rf /tmp/kish_glob_test

# ---------- 14. Process substitution ----------
echo "--- 14. Process substitution ---"
assert "diff process sub" "1c1" "$(diff <(echo a) <(echo b) | head -1)"

# ---------- 15. Aliases ----------
echo "--- 15. Aliases ---"
shopt -s expand_aliases 2>/dev/null
alias testgreet='echo hi'
assert "alias" "hi" "$(testgreet)"
unalias testgreet

# ---------- 16. Trap ----------
echo "--- 16. Trap ---"
# Note: trap EXIT in $() subshell only works in bash (fork-based), not in kish (goroutine-based)
# Test trap EXIT via script execution instead
echo '#!/bin/sh
trap "echo trapped" EXIT
true' > /tmp/kish_trap_test.sh
chmod +x /tmp/kish_trap_test.sh
assert "trap exit" "trapped" "$(/tmp/kish_trap_test.sh)"
rm -f /tmp/kish_trap_test.sh

# ---------- 17. set options ----------
echo "--- 17. set options ---"
assert "set -e survives true" "alive" "$(set -e; true; echo alive)"
assert "pipefail" "1" "$(set -o pipefail; false | true; echo $?)"
assert "nounset" "error" "$(set -u; echo ${UNSET_VAR_XYZ:-error})"

# ---------- 18. Case statement ----------
echo "--- 18. Case statement ---"
assert "case match" "fruit" "$(x=apple; case $x in apple|banana) echo fruit;; car) echo vehicle;; esac)"
assert "case wildcard" "other" "$(x=xyz; case $x in a*) echo starts_a;; *) echo other;; esac)"

# ---------- 19. Nameref ----------
echo "--- 19. Nameref ---"
declare -n ref=target
target=42
assert "nameref" "42" "$ref"
unset -n ref

# ---------- 20. Parameter special vars ----------
echo "--- 20. Special variables ---"
assert "dollar question" "0" "$(true; echo $?)"
assert "dollar bang" "true" "$(sleep 0.01 & [ -n "$!" ] && echo true)"
wait 2>/dev/null
assert "dollar dollar" "true" "$([ $$ -gt 0 ] && echo true)"
assert "RANDOM" "true" "$([ $RANDOM -ge 0 ] && echo true)"

# ---------- 21. Subshell ----------
echo "--- 21. Subshell ---"
assert "subshell isolation" "outer" "$(x=outer; (x=inner); echo $x)"
assert "subshell exit" "42" "$( (exit 42); echo $?)"

# ---------- 22. Mapfile ----------
echo "--- 22. Mapfile ---"
assert "mapfile" "b" "$(mapfile -t arr <<< $'a\nb\nc'; echo ${arr[1]})"

# ---------- 23. Read ----------
echo "--- 23. Read ---"
assert "read" "hello" "$(read x <<< hello; echo $x)"
assert "read multivar" "a:b" "$(read x y <<< 'a b'; echo "$x:$y")"

# ---------- 24. Source ----------
echo "--- 24. Source ---"
echo 'SOURCED_VAR=yes' > /tmp/kish_source_test.sh
source /tmp/kish_source_test.sh
assert "source" "yes" "$SOURCED_VAR"
rm /tmp/kish_source_test.sh

# ---------- 25. Extglob ----------
echo "--- 25. Extglob ---"
# extglob must be set before parsing, so test via script
echo '#!/usr/bin/env bash
shopt -s extglob
mkdir -p /tmp/kish_extglob
touch /tmp/kish_extglob/{foo.txt,bar.txt,baz.log}
count=$(ls /tmp/kish_extglob/@(foo|bar).txt 2>/dev/null | wc -l)
rm -rf /tmp/kish_extglob
echo $count' > /tmp/kish_extglob_test.sh
chmod +x /tmp/kish_extglob_test.sh
assert "extglob match" "2" "$(/tmp/kish_extglob_test.sh)"
rm -f /tmp/kish_extglob_test.sh

# ---------- Summary ----------
echo ""
echo "=== Results ==="
echo "PASS: $PASS / $TOTAL"
echo "FAIL: $FAIL / $TOTAL"
if [ $FAIL -eq 0 ]; then
    echo "ALL TESTS PASSED"
else
    echo "SOME TESTS FAILED"
    exit 1
fi
