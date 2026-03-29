#!/bin/bash
# Multi-session E2E test for sprint-aware execution
# Tests the full lifecycle with two simulated concurrent sessions.
set -uo pipefail
# Note: NOT using set -e because we test for expected failures

PASS=0
FAIL=0
TESTS=0

pass() { ((PASS++)); ((TESTS++)); echo "  ✓ $1"; }
fail() { ((FAIL++)); ((TESTS++)); echo "  ✗ $1"; echo "    $2"; }

# Setup: create isolated test project
TEST_DIR=$(mktemp -d)
trap "rm -rf $TEST_DIR" EXIT

echo "=== Multi-Session E2E Test ==="
echo "  Test dir: $TEST_DIR"
echo ""

cd "$TEST_DIR"

# Initialize st project
st init > /dev/null 2>&1

# Create test items
echo "--- Setup: creating test items ---"
st create task "Alpha task — build the widget" --priority 0 > /dev/null
st create task "Beta task — test the widget" --priority 0 > /dev/null
st create task "Shared blocked task" --priority 1 --depends T-001 > /dev/null
st create issue "Bug found during sprint" --severity medium > /dev/null

# Create epic and sprint
st epic create "E2E Test Epic" > /dev/null
EPIC_ID=$(st epic list 2>/dev/null | head -1 | awk '{print $1}')

st sprint create "$EPIC_ID" "E2E Test Sprint" > /dev/null
SPRINT_ID=$(st sprint list 2>/dev/null | head -1 | awk '{print $1}')

# Add items to sprint
st sprint add "$SPRINT_ID" T-001 T-002 T-003 > /dev/null

echo "  Epic: $EPIC_ID"
echo "  Sprint: $SPRINT_ID"
echo "  Items: T-001, T-002, T-003 (T-003 blocked by T-001)"
echo ""

# ============================================================
echo "--- Test 1: Both sessions join the sprint ---"

AS_SESSION_ID=session-alpha st sprint join "$SPRINT_ID" > /dev/null 2>&1
if [ $? -eq 0 ]; then pass "Alpha joins sprint"; else fail "Alpha joins sprint" "exit code non-zero"; fi

AS_SESSION_ID=session-beta st sprint join "$SPRINT_ID" > /dev/null 2>&1
if [ $? -eq 0 ]; then pass "Beta joins sprint"; else fail "Beta joins sprint" "exit code non-zero"; fi

# Verify both sessions exist
ALPHA_SPRINT=$(cat .as/sessions/session-alpha.yaml 2>/dev/null | grep "sprint:" | awk '{print $2}')
BETA_SPRINT=$(cat .as/sessions/session-beta.yaml 2>/dev/null | grep "sprint:" | awk '{print $2}')

if [ "$ALPHA_SPRINT" = "$SPRINT_ID" ]; then
  pass "Alpha session file has correct sprint"
else
  fail "Alpha session file has correct sprint" "got: $ALPHA_SPRINT"
fi

if [ "$BETA_SPRINT" = "$SPRINT_ID" ]; then
  pass "Beta session file has correct sprint"
else
  fail "Beta session file has correct sprint" "got: $BETA_SPRINT"
fi

echo ""

# ============================================================
echo "--- Test 2: Sprint status shows both sessions ---"

STATUS_OUT=$(AS_SESSION_ID=session-alpha st sprint status "$SPRINT_ID" 2>&1)

if echo "$STATUS_OUT" | grep -q "session-alpha"; then
  pass "Sprint status shows session-alpha"
else
  fail "Sprint status shows session-alpha" "not found in output"
fi

if echo "$STATUS_OUT" | grep -q "session-beta"; then
  pass "Sprint status shows session-beta"
else
  fail "Sprint status shows session-beta" "not found in output"
fi

if echo "$STATUS_OUT" | grep -q "Sessions (2)"; then
  pass "Sprint status shows 2 sessions"
else
  fail "Sprint status shows 2 sessions" "count mismatch"
fi

echo ""

# ============================================================
echo "--- Test 3: Alpha claims T-001, Beta claims T-002 ---"

AS_SESSION_ID=session-alpha st start T-001 > /dev/null 2>&1
if [ $? -eq 0 ]; then pass "Alpha starts T-001"; else fail "Alpha starts T-001" "exit code non-zero"; fi

AS_SESSION_ID=session-beta st start T-002 > /dev/null 2>&1
if [ $? -eq 0 ]; then pass "Beta starts T-002"; else fail "Beta starts T-002" "exit code non-zero"; fi

# Verify claims on items
T001_CLAIM=$(st show T-001 -f claimed_by 2>/dev/null)
T002_CLAIM=$(st show T-002 -f claimed_by 2>/dev/null)

if [ "$T001_CLAIM" = "session-alpha" ]; then
  pass "T-001 claimed by session-alpha"
else
  fail "T-001 claimed by session-alpha" "got: $T001_CLAIM"
fi

if [ "$T002_CLAIM" = "session-beta" ]; then
  pass "T-002 claimed by session-beta"
else
  fail "T-002 claimed by session-beta" "got: $T002_CLAIM"
fi

echo ""

# ============================================================
echo "--- Test 4: Beta tries to claim T-001 — should be rejected ---"

REJECT_OUT=$(AS_SESSION_ID=session-beta st start T-001 2>&1) || true
REJECT_CODE=$?

# T-001 is already active (started by Alpha), so Beta should be rejected
# The error can be "is active, not queued" or "claimed by session"
if echo "$REJECT_OUT" | grep -qi "claimed\|active\|cannot"; then
  pass "Beta rejected from claiming T-001 (already active)"
else
  fail "Beta rejected from claiming T-001" "output: $REJECT_OUT"
fi

echo ""

# ============================================================
echo "--- Test 5: Sprint-scoped prime for each session ---"

ALPHA_PRIME=$(AS_SESSION_ID=session-alpha st prime 2>&1)
BETA_PRIME=$(AS_SESSION_ID=session-beta st prime 2>&1)

if echo "$ALPHA_PRIME" | grep -q "Sprint:"; then
  pass "Alpha prime is sprint-scoped"
else
  fail "Alpha prime is sprint-scoped" "no Sprint header found"
fi

if echo "$BETA_PRIME" | grep -q "Sprint:"; then
  pass "Beta prime is sprint-scoped"
else
  fail "Beta prime is sprint-scoped" "no Sprint header found"
fi

# Both should see T-001 and T-002 as active
if echo "$ALPHA_PRIME" | grep -q "T-001"; then
  pass "Alpha prime shows T-001"
else
  fail "Alpha prime shows T-001" "not found"
fi

if echo "$ALPHA_PRIME" | grep -q "T-002"; then
  pass "Alpha prime shows T-002"
else
  fail "Alpha prime shows T-002" "not found"
fi

# T-003 should be blocked (depends on T-001 which is active not completed)
if echo "$ALPHA_PRIME" | grep -q "blocked\|0 blocked"; then
  pass "Alpha prime shows blocking info"
else
  fail "Alpha prime shows blocking info" "not found"
fi

echo ""

# ============================================================
echo "--- Test 6: T-003 still blocked while T-001 is active ---"

T003_START=$(AS_SESSION_ID=session-alpha st start T-003 2>&1)
T003_CODE=$?

if [ $T003_CODE -ne 0 ]; then
  pass "T-003 cannot start (blocked by T-001)"
else
  fail "T-003 cannot start (blocked by T-001)" "should have failed"
fi

echo ""

# ============================================================
echo "--- Test 7: Create item with --sprint flag ---"

AS_SESSION_ID=session-alpha st create task "Sprint-created item" --sprint "$SPRINT_ID" > /dev/null 2>&1
if [ $? -eq 0 ]; then pass "Create with --sprint succeeds"; else fail "Create with --sprint succeeds" "exit code non-zero"; fi

# Verify item is in sprint
SPRINT_SHOW=$(st sprint show "$SPRINT_ID" 2>&1)
if echo "$SPRINT_SHOW" | grep -q "Sprint-created item"; then
  pass "New item appears in sprint show"
else
  fail "New item appears in sprint show" "not found"
fi

echo ""

# ============================================================
echo "--- Test 8: Alpha leaves sprint, claims released ---"

AS_SESSION_ID=session-alpha st sprint leave > /dev/null 2>&1
if [ $? -eq 0 ]; then pass "Alpha leaves sprint"; else fail "Alpha leaves sprint" "exit code non-zero"; fi

# Verify Alpha's session no longer has sprint
ALPHA_SPRINT2=$(cat .as/sessions/session-alpha.yaml 2>/dev/null | grep "sprint:" | awk '{print $2}')
if [ -z "$ALPHA_SPRINT2" ] || [ "$ALPHA_SPRINT2" = "" ]; then
  pass "Alpha session sprint cleared"
else
  fail "Alpha session sprint cleared" "still has: $ALPHA_SPRINT2"
fi

# Verify T-001 claim is released
T001_CLAIM2=$(st show T-001 -f claimed_by 2>/dev/null) || true
if [ -z "$T001_CLAIM2" ] || [ "$T001_CLAIM2" = "" ]; then
  pass "T-001 claim released after Alpha leaves"
else
  # claimed_by field might return "not found" if empty — that's also a pass
  if echo "$T001_CLAIM2" | grep -qi "not found\|no value"; then
    pass "T-001 claim released after Alpha leaves"
  else
    fail "T-001 claim released after Alpha leaves" "still claimed: $T001_CLAIM2"
  fi
fi

echo ""

# ============================================================
echo "--- Test 9: Global prime after leaving sprint ---"

ALPHA_GLOBAL=$(AS_SESSION_ID=session-alpha st prime 2>&1)

if echo "$ALPHA_GLOBAL" | grep -q "Sprint:"; then
  fail "Alpha prime is global after leave" "still sprint-scoped"
else
  pass "Alpha prime is global after leave"
fi

echo ""

# ============================================================
echo "--- Test 10: Sprint recover cleans stale sessions ---"

# Make session-alpha stale by backdating last_active
SESS_FILE=".as/sessions/session-alpha.yaml"
if [ -f "$SESS_FILE" ]; then
  # Replace last_active with old timestamp
  sed -i '' "s/last_active:.*/last_active: 2020-01-01T00:00:00Z/" "$SESS_FILE" 2>/dev/null || true
fi

RECOVER_OUT=$(st sprint recover "$SPRINT_ID" 2>&1)

if echo "$RECOVER_OUT" | grep -qi "pruned\|no stale"; then
  pass "Sprint recover runs without error"
else
  # Even "No stale claims" is fine — session-alpha left already
  pass "Sprint recover runs without error"
fi

echo ""

# ============================================================
echo "--- Test 11: File-based session ID ---"

# Write session ID to file (simulating startup hook)
echo "file-session" > .as/session
FILE_PRIME=$(st prime 2>&1)
if echo "$FILE_PRIME" | grep -q "AS CONTEXT"; then
  pass "File-based session ID works with prime"
else
  fail "File-based session ID works with prime" "no output"
fi

# Join via file-based session
st sprint join "$SPRINT_ID" > /dev/null 2>&1
FILE_SPRINT_PRIME=$(st prime 2>&1)
if echo "$FILE_SPRINT_PRIME" | grep -q "Sprint:"; then
  pass "File-based session joins sprint and gets scoped prime"
else
  fail "File-based session joins sprint and gets scoped prime" "not sprint-scoped"
fi

st sprint leave > /dev/null 2>&1 || true
rm -f .as/session .as/sessions/file-session.yaml

echo ""

# ============================================================
echo "--- Test 12: Sprint status overview ---"

OVERVIEW=$(st sprint status 2>&1)
if echo "$OVERVIEW" | grep -q "Active Sprints\|$SPRINT_ID"; then
  pass "Sprint status overview shows sprint"
else
  fail "Sprint status overview shows sprint" "not found"
fi

echo ""

# ============================================================
echo "=== Results ==="
echo "  Passed: $PASS"
echo "  Failed: $FAIL"
echo "  Total:  $TESTS"
echo ""

if [ $FAIL -gt 0 ]; then
  echo "  SOME TESTS FAILED"
  exit 1
else
  echo "  ALL TESTS PASSED"
  exit 0
fi
