#!/usr/bin/env bash
# End-to-end exercise of every Phase 1 endpoint.
#
#   Terminal 1:  cd backend && go run ./cmd/server
#   Terminal 2:  bash backend/scripts/api-smoke.sh
#
# Needs curl and python (jq is not required). Every request is a plain curl
# call, printed before it runs, so this doubles as a readable list of the
# commands rather than a black box.

set -u

BASE="${BASE:-http://localhost:8080}"
PASS=0
FAIL=0

# Cookie jars stand in for two different browsers. The voter cookie is what
# the duplicate-vote check keys on, so a second jar is a second person.
JAR_A="$(mktemp)"
JAR_B="$(mktemp)"
trap 'rm -f "$JAR_A" "$JAR_B"' EXIT

# --- tiny helpers -----------------------------------------------------------

# json <field> reads a top-level-ish field out of a JSON blob on stdin.
# Dotted paths are supported, e.g. json poll.slug
json() {
  python -c '
import json,sys
doc = json.load(sys.stdin)
for part in sys.argv[1].split("."):
    doc = doc[part] if isinstance(doc, dict) else doc[int(part)]
print(doc)
' "$1" 2>/dev/null
}

# check <label> <expected-status> <actual-status> [body]
check() {
  local label="$1" want="$2" got="$3" body="${4:-}"
  if [ "$got" = "$want" ]; then
    printf '  \033[32mPASS\033[0m  %-46s %s\n' "$label" "$got"
    PASS=$((PASS + 1))
  else
    printf '  \033[31mFAIL\033[0m  %-46s got %s, want %s\n' "$label" "$got" "$want"
    [ -n "$body" ] && printf '        %s\n' "$body"
    FAIL=$((FAIL + 1))
  fi
}

# req <method> <path> [json-body] [extra curl args...]
# Prints "<body>\n<status>" so callers can split them.
req() {
  local method="$1" path="$2" body="${3:-}"
  shift 3 2>/dev/null || shift 2
  if [ -n "$body" ]; then
    curl -sS -o /dev/null -w '%{http_code}' -X "$method" "$BASE$path" \
      -H 'Content-Type: application/json' -d "$body" "$@"
  else
    curl -sS -o /dev/null -w '%{http_code}' -X "$method" "$BASE$path" "$@"
  fi
}

# body_of <method> <path> [json-body] [extra curl args...]  -> response body
body_of() {
  local method="$1" path="$2" body="${3:-}"
  shift 3 2>/dev/null || shift 2
  if [ -n "$body" ]; then
    curl -sS -X "$method" "$BASE$path" -H 'Content-Type: application/json' -d "$body" "$@"
  else
    curl -sS -X "$method" "$BASE$path" "$@"
  fi
}

section() { printf '\n\033[1m%s\033[0m\n' "$1"; }

# --- 1. health --------------------------------------------------------------

section '1. Health'

HEALTH_BODY="$(body_of GET /healthz)"
HEALTH_CODE="$(req GET /healthz)"
check 'GET /healthz' 200 "$HEALTH_CODE" "$HEALTH_BODY"
printf '        %s\n' "$HEALTH_BODY"

if [ "$HEALTH_CODE" != "200" ]; then
  printf '\n\033[31mServer is not healthy. Is it running, and is MONGODB_URI set?\033[0m\n'
  exit 1
fi

# --- 2. auth ----------------------------------------------------------------

section '2. Auth'

STAMP="$(date +%s)"
EMAIL="alice+$STAMP@example.com"
EMAIL2="bob+$STAMP@example.com"
PASSWORD='correct-horse-battery-staple'

SIGNUP_BODY="$(body_of POST /api/auth/signup "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")"
check 'POST /api/auth/signup' 201 \
  "$(req POST /api/auth/signup "{\"email\":\"dup$STAMP@example.com\",\"password\":\"$PASSWORD\"}")"

TOKEN="$(printf '%s' "$SIGNUP_BODY" | json token)"
if [ -z "$TOKEN" ]; then
  printf '\033[31mNo token in signup response:\033[0m %s\n' "$SIGNUP_BODY"
  exit 1
fi

check 'POST /api/auth/signup  (duplicate email -> 409)' 409 \
  "$(req POST /api/auth/signup "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")"

check 'POST /api/auth/signup  (short password -> 400)' 400 \
  "$(req POST /api/auth/signup "{\"email\":\"x$STAMP@example.com\",\"password\":\"short\"}")"

check 'POST /api/auth/signup  (bad email -> 400)' 400 \
  "$(req POST /api/auth/signup "{\"email\":\"not-an-email\",\"password\":\"$PASSWORD\"}")"

check 'POST /api/auth/login' 200 \
  "$(req POST /api/auth/login "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")"

check 'POST /api/auth/login   (wrong password -> 401)' 401 \
  "$(req POST /api/auth/login "{\"email\":\"$EMAIL\",\"password\":\"wrong-password\"}")"

check 'POST /api/auth/login   (unknown email -> 401)' 401 \
  "$(req POST /api/auth/login "{\"email\":\"ghost$STAMP@example.com\",\"password\":\"$PASSWORD\"}")"

check 'GET  /api/auth/me      (with token)' 200 \
  "$(req GET /api/auth/me '' -H "Authorization: Bearer $TOKEN")"

check 'GET  /api/auth/me      (no token -> 401)' 401 "$(req GET /api/auth/me)"

check 'GET  /api/auth/me      (garbage token -> 401)' 401 \
  "$(req GET /api/auth/me '' -H 'Authorization: Bearer not.a.real.token')"

# --- 3. poll creation + validation -----------------------------------------

section '3. Poll creation and validation'

POLL_BODY="$(body_of POST /api/polls \
  '{"question":"What should we build next?","options":["Dark mode","CSV export","Mobile app"]}' \
  -H "Authorization: Bearer $TOKEN")"

SLUG="$(printf '%s' "$POLL_BODY" | json poll.slug)"
POLL_ID="$(printf '%s' "$POLL_BODY" | json poll.id)"

if [ -z "$SLUG" ]; then
  printf '\033[31mNo slug in create response:\033[0m %s\n' "$POLL_BODY"
  exit 1
fi
printf '        slug=%s  id=%s\n' "$SLUG" "$POLL_ID"

check 'POST /api/polls        (valid -> 201)' 201 \
  "$(req POST /api/polls '{"question":"Second poll","options":["A","B"]}' \
     -H "Authorization: Bearer $TOKEN")"

check 'POST /api/polls        (no auth -> 401)' 401 \
  "$(req POST /api/polls '{"question":"Nope","options":["A","B"]}')"

check 'POST /api/polls        (1 option -> 400)' 400 \
  "$(req POST /api/polls '{"question":"Too few","options":["Only one"]}' \
     -H "Authorization: Bearer $TOKEN")"

check 'POST /api/polls        (11 options -> 400)' 400 \
  "$(req POST /api/polls '{"question":"Too many","options":["1","2","3","4","5","6","7","8","9","10","11"]}' \
     -H "Authorization: Bearer $TOKEN")"

check 'POST /api/polls        (duplicate options -> 400)' 400 \
  "$(req POST /api/polls '{"question":"Dupes","options":["Yes","  yes  "]}' \
     -H "Authorization: Bearer $TOKEN")"

check 'POST /api/polls        (blank option -> 400)' 400 \
  "$(req POST /api/polls '{"question":"Blank","options":["Yes","   "]}' \
     -H "Authorization: Bearer $TOKEN")"

check 'POST /api/polls        (empty question -> 400)' 400 \
  "$(req POST /api/polls '{"question":"","options":["A","B"]}' \
     -H "Authorization: Bearer $TOKEN")"

check 'POST /api/polls        (past expiry -> 400)' 400 \
  "$(req POST /api/polls '{"question":"Expired already","options":["A","B"],"expiresAt":"2020-01-01T00:00:00Z"}' \
     -H "Authorization: Bearer $TOKEN")"

check 'POST /api/polls        (malformed JSON -> 400)' 400 \
  "$(req POST /api/polls '{"question":' -H "Authorization: Bearer $TOKEN")"

# --- 4. public reads --------------------------------------------------------

section '4. Public reads'

check 'GET  /api/polls/:slug  (public, no auth)' 200 "$(req GET "/api/polls/$SLUG")"
check 'GET  /api/polls/:id    (ObjectID also works)' 200 "$(req GET "/api/polls/$POLL_ID")"
check 'GET  /api/polls/:id    (unknown -> 404)' 404 "$(req GET '/api/polls/does-not-exist')"
check 'GET  /api/polls        (own polls, auth)' 200 \
  "$(req GET /api/polls '' -H "Authorization: Bearer $TOKEN")"
check 'GET  /api/polls        (no auth -> 401)' 401 "$(req GET /api/polls)"

# --- 5. voting --------------------------------------------------------------

section '5. Voting and duplicate prevention'

VOTE_BODY="$(body_of POST "/api/polls/$SLUG/vote" '{"optionIndexes":[1]}' -c "$JAR_A" -b "$JAR_A")"
check 'POST /api/polls/:id/vote  (voter A, first vote)' 201 \
  "$(req POST "/api/polls/$SLUG/vote" '{"optionIndexes":[0]}' -c "$JAR_B" -b "$JAR_B")"
printf '        voter A response: %s\n' "$VOTE_BODY"

check 'POST /api/polls/:id/vote  (voter A again -> 409)' 409 \
  "$(req POST "/api/polls/$SLUG/vote" '{"optionIndexes":[2]}' -c "$JAR_A" -b "$JAR_A")"

check 'POST /api/polls/:id/vote  (voter B again -> 409)' 409 \
  "$(req POST "/api/polls/$SLUG/vote" '{"optionIndexes":[1]}' -c "$JAR_B" -b "$JAR_B")"

check 'POST /api/polls/:id/vote  (option 99 -> 400)' 400 \
  "$(req POST "/api/polls/$SLUG/vote" '{"optionIndexes":[99]}')"

check 'POST /api/polls/:id/vote  (negative index -> 400)' 400 \
  "$(req POST "/api/polls/$SLUG/vote" '{"optionIndexes":[-1]}')"

check 'POST /api/polls/:id/vote  (missing index -> 400)' 400 \
  "$(req POST "/api/polls/$SLUG/vote" '{"optionIndexes":[]}')"

check 'POST /api/polls/:id/vote  (unknown poll -> 404)' 404 \
  "$(req POST '/api/polls/nosuchpoll/vote' '{"optionIndexes":[0]}')"

printf '        tally now: %s\n' "$(body_of GET "/api/polls/$SLUG")"

# --- 5b. multiple-choice mode ----------------------------------------------

section '5b. Multiple-choice polls'

MULTI_BODY="$(body_of POST /api/polls \
  '{"question":"Which languages do you use?","options":["Go","JavaScript","Python","Rust"],"mode":"multiple"}' \
  -H "Authorization: Bearer $TOKEN")"
MULTI_SLUG="$(printf '%s' "$MULTI_BODY" | json poll.slug)"
MULTI_MODE="$(printf '%s' "$MULTI_BODY" | json poll.mode)"

check 'POST /api/polls        (mode=multiple -> 201)' 201 \
  "$(req POST /api/polls '{"question":"Another multi","options":["A","B"],"mode":"multiple"}' \
     -H "Authorization: Bearer $TOKEN")"

check 'POST /api/polls        (bad mode -> 400)' 400 \
  "$(req POST /api/polls '{"question":"Bad mode","options":["A","B"],"mode":"ranked"}' \
     -H "Authorization: Bearer $TOKEN")"

if [ "$MULTI_MODE" = "multiple" ]; then
  printf '  \033[32mPASS\033[0m  %-46s %s\n' 'created poll reports mode=multiple' "$MULTI_MODE"
  PASS=$((PASS + 1))
else
  printf '  \033[31mFAIL\033[0m  %-46s got %s\n' 'created poll reports mode=multiple' "$MULTI_MODE"
  FAIL=$((FAIL + 1))
fi

JAR_C="$(mktemp)"
check 'POST vote  (3 selections on a multi poll -> 201)' 201 \
  "$(req POST "/api/polls/$MULTI_SLUG/vote" '{"optionIndexes":[0,2,3]}' -c "$JAR_C" -b "$JAR_C")"

check 'POST vote  (same voter again -> 409)' 409 \
  "$(req POST "/api/polls/$MULTI_SLUG/vote" '{"optionIndexes":[1]}' -c "$JAR_C" -b "$JAR_C")"

check 'POST vote  (repeated index [1,1] -> 400)' 400 \
  "$(req POST "/api/polls/$MULTI_SLUG/vote" '{"optionIndexes":[1,1]}')"

check 'POST vote  (2 selections on a SINGLE poll -> 400)' 400 \
  "$(req POST "/api/polls/$SLUG/vote" '{"optionIndexes":[0,1]}')"

# One ballot naming three options must add 3 selections but only 1 voter.
MULTI_AFTER="$(body_of GET "/api/polls/$MULTI_SLUG")"
MULTI_VOTERS="$(printf '%s' "$MULTI_AFTER" | json poll.totalVotes)"
MULTI_SELECTIONS="$(printf '%s' "$MULTI_AFTER" | json poll.totalSelections)"
check 'one 3-option ballot counts as 1 voter' 1 "$MULTI_VOTERS" "$MULTI_AFTER"
check 'one 3-option ballot counts as 3 selections' 3 "$MULTI_SELECTIONS" "$MULTI_AFTER"
rm -f "$JAR_C"

# --- 6. ownership -----------------------------------------------------------

section '6. Ownership enforcement'

TOKEN2="$(body_of POST /api/auth/signup \
  "{\"email\":\"$EMAIL2\",\"password\":\"$PASSWORD\"}" | json token)"

check "PATCH /api/polls/:id/close  (stranger -> 403)" 403 \
  "$(req PATCH "/api/polls/$SLUG/close" '' -H "Authorization: Bearer $TOKEN2")"

check "DELETE /api/polls/:id       (stranger -> 403)" 403 \
  "$(req DELETE "/api/polls/$SLUG" '' -H "Authorization: Bearer $TOKEN2")"

check "PATCH /api/polls/:id/close  (no auth -> 401)" 401 \
  "$(req PATCH "/api/polls/$SLUG/close")"

# --- 7. closing -------------------------------------------------------------

section '7. Closing a poll'

check 'PATCH /api/polls/:id/close  (owner)' 200 \
  "$(req PATCH "/api/polls/$SLUG/close" '' -H "Authorization: Bearer $TOKEN")"

check 'PATCH /api/polls/:id/close  (already closed -> 409)' 409 \
  "$(req PATCH "/api/polls/$SLUG/close" '' -H "Authorization: Bearer $TOKEN")"

check 'POST  /api/polls/:id/vote   (closed poll -> 409)' 409 \
  "$(req POST "/api/polls/$SLUG/vote" '{"optionIndexes":[0]}')"

# --- 8. deleting ------------------------------------------------------------

section '8. Deleting a poll'

check 'DELETE /api/polls/:id  (owner -> 204)' 204 \
  "$(req DELETE "/api/polls/$SLUG" '' -H "Authorization: Bearer $TOKEN")"

check 'GET    /api/polls/:id  (after delete -> 404)' 404 "$(req GET "/api/polls/$SLUG")"

# --- 9. misc ----------------------------------------------------------------

section '9. Router behaviour'

check 'GET  /api/nope         (unknown route -> 404)' 404 "$(req GET /api/nope)"
check 'OPTIONS preflight, allowed origin -> 204' 204 \
  "$(req OPTIONS /api/polls '' -H 'Origin: http://localhost:5173' -H 'Access-Control-Request-Method: POST')"

# --- summary ----------------------------------------------------------------

printf '\n\033[1mSummary:\033[0m %d passed, %d failed\n\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
