# Live Polls

Create a poll, share the link, and watch the results move as people vote.
No refresh, no polling — results are pushed over a WebSocket.

**Live:** _add your deployed URL here_
**Repo:** https://github.com/Monashini/live-polls

<!--
  SCREENSHOT PLACEHOLDER
  Replace with a real screenshot before submitting. The most convincing one is
  two browser windows side by side on the same poll, mid-vote, so the live
  update is visible in a still image.

  ![Two windows showing the same poll updating live](docs/screenshot.png)
-->

_Screenshot goes here._

---

## What it does

- Sign up / log in. An account is needed to **create** a poll, never to vote.
- Create a poll with 2–10 options, single-choice or multiple-choice.
- Share a short random link. Anyone who opens it can vote, no account.
- Everyone watching sees counts update live.
- One vote per person per poll, enforced by the database.
- Close a poll or delete it; open viewers are told immediately.

## Stack

| Layer | Choice | What it actually does here |
| --- | --- | --- |
| Frontend | React 19 + Vite | Auth, poll creation, voting, live results |
| Backend | Go 1.26 + Gin | REST API, validation, auth, WebSocket hub |
| Database | MongoDB | Source of truth: users, polls, votes |
| Realtime | Redis | Live counters, Pub/Sub fan-out, rate limiting |

## Layout

```
backend/
  cmd/server/          main() — config, wiring, graceful shutdown. Nothing else.
  internal/
    config/            every env var, read and validated in one place
    apperr/            the one error type that crosses layer boundaries
    models/            structs, BSON/JSON tags, and the API view types
    db/                the only package that imports the mongo driver
    redisstore/        the only package that imports the redis client
    ws/                WebSocket hub and connection lifecycle
    services/          all business rules and validation
    handlers/          HTTP in, JSON out. No rules, no queries.
    middleware/        request ID, logging, recovery, CORS, timeouts, auth
  scripts/
    api-smoke.sh       51 end-to-end assertions against a running server

frontend/
  src/
    api/               the only code that calls fetch
    auth/              session context, useAuth hook, route guard
    hooks/             useAsyncData, useWebSocket, useLivePoll
    lib/               validation mirrors and formatting
    components/        small, presentational, no network access
    pages/             one per route; all data fetching happens here
```

The dependency direction is one-way: `handlers → services → db / redisstore`.
A handler cannot reach a collection and a service has no idea HTTP exists,
which is what lets the same vote logic serve a REST call today and anything
else later.

---

## Running it locally

### Option A — Docker for the databases (simplest)

```bash
docker compose up -d
```

That starts MongoDB on 27017 and Redis on 6379. Then:

```bash
cd backend
cp .env.example .env
```

Set `MONGODB_URI=mongodb://localhost:27017` and `REDIS_URL=redis://localhost:6379`
(one `s` — local Redis has no TLS), and generate the two secrets:

```bash
openssl rand -base64 48
```

### Option B — hosted free tiers, no Docker

Create a free **MongoDB Atlas** M0 cluster and a free **Upstash** Redis
database, and paste their connection strings instead. Upstash requires
`rediss://` with two esses.

This is what I did, because Docker Desktop on Windows Home needs WSL2 and I
would rather develop against the same infrastructure I deploy to.

### Run both halves

```bash
cd backend && go run ./cmd/server
```

```bash
cd frontend && npm install && npm run dev
```

Open http://localhost:5173.

Startup connects to MongoDB and Redis, pings both, and creates indexes before
binding a port — a bad connection string fails immediately and loudly instead
of on the first request.

```bash
curl http://localhost:8080/healthz
# {"mongo":"ok","redis":"ok","status":"ok","wsClients":0,"wsRooms":0}
```

### Tests

```bash
cd backend && go test ./...          # routing, guards, CORS, WS origin checks
bash backend/scripts/api-smoke.sh    # 51 assertions against a running server
```

> **Windows note:** if `go test` fails with "An Application Control policy has
> blocked this file", Smart App Control is blocking freshly built binaries.
> `go run` works, or build to a fixed path and run that.

---

## Environment variables

### Backend (`backend/.env`)

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `MONGODB_URI` | yes | — | Connection string |
| `MONGODB_DATABASE` | no | `livepolls` | Database name |
| `REDIS_URL` | yes | — | `rediss://` for hosted, `redis://` for local |
| `JWT_SECRET` | yes | — | Min 32 chars. Signs access tokens |
| `JWT_TTL` | no | `24h` | Token lifetime |
| `IP_HASH_SALT` | no | derived from `JWT_SECRET` | Salts stored IP hashes |
| `CORS_ALLOWED_ORIGINS` | no | — | Comma-separated exact origins. No wildcards |
| `COOKIE_SECURE` | no | `true` in prod | Voter cookie Secure flag |
| `COOKIE_SAMESITE` | no | `lax` | Must be `none` when frontend and API are on different domains |
| `COOKIE_DOMAIN` | no | — | Only if they share a parent domain |
| `TRUSTED_PROXIES` | no | trust nobody | Must be set behind a platform proxy |
| `REDIS_COUNTS_TTL` | no | `24h` | How long idle poll counters stay cached |
| `VOTE_RATE_LIMIT` | no | `30` | Votes per window per IP |
| `VOTE_RATE_WINDOW` | no | `5m` | The window |
| `APP_ENV` | no | `development` | `production` switches Gin to release mode |
| `PORT` | no | `8080` | Set by the host platform |

### Frontend (`frontend/.env`)

| Variable | Purpose |
| --- | --- |
| `VITE_API_BASE_URL` | Backend origin, no trailing slash. Must be `https://` in production, or the WebSocket URL derived from it will be blocked as mixed content |

Anything prefixed `VITE_` is compiled into the bundle and is therefore public.
No secret ever goes in that file.

---

## API

All responses are JSON. Errors use one envelope:

```json
{ "error": { "code": "VALIDATION_FAILED", "message": "...", "fields": {...}, "requestId": "a1b2c3" } }
```

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| GET | `/healthz` | – | Real MongoDB + Redis pings, WebSocket counts |
| POST | `/api/auth/signup` | – | Create account, returns a token |
| POST | `/api/auth/login` | – | Exchange credentials for a token |
| GET | `/api/auth/me` | bearer | The account behind the token |
| POST | `/api/polls` | bearer | Create a poll |
| GET | `/api/polls` | bearer | The caller's own polls |
| GET | `/api/polls/:id` | – | A poll and its current results |
| PATCH | `/api/polls/:id/close` | owner | Stop accepting votes |
| DELETE | `/api/polls/:id` | owner | Delete a poll |
| POST | `/api/polls/:id/vote` | – | Cast one ballot |
| GET | `/ws/polls/:id` | – | Live results feed |

`:id` accepts the poll's ObjectID **or** its share slug.

Create takes an optional `"mode": "single" | "multiple"` (default `single`).
A ballot is always a list, in both modes:

```json
POST /api/polls/:id/vote
{ "optionIndexes": [0, 2] }
```

WebSocket messages are the same shape as the REST payload, wrapped:

```json
{ "type": "results" | "closed" | "deleted", "pollId": "...", "poll": { ... } }
```

---

## Architecture

### The Mongo / Redis split

**MongoDB owns the truth.** Every vote is a durable document. If Redis were
wiped entirely, nothing would be lost — every number in the app can be rebuilt
from the `votes` collection.

**Redis owns the live layer**, and does three separate jobs:

1. **Counters.** A hash per poll (`livepolls:poll:{id}:counts`) with a field
   per option plus a `voters` count. Reading results is one Redis round trip
   instead of an aggregation over every vote ever cast.
2. **Pub/Sub fan-out.** One channel per poll.
3. **Rate limiting.** A per-IP counter with a TTL.

The counter increment is a **Lua script**, not a pipeline, and that is not
incidental. `EXISTS` followed by `HINCRBY` as two commands has a gap: if the key
expires in between, `HINCRBY` creates a fresh hash containing only that one
vote — silently resetting a poll from 400 votes to 1. Lua makes the check and
the increment one atomic operation. An empty reply means the key is genuinely
gone, which is the signal to reseed from MongoDB.

### Write order on a vote

```
rate limit → validate → insert vote (Mongo) → $inc tally (Mongo)
                                   → HINCRBY counters (Redis) → PUBLISH
```

The vote document is written **first**, because its unique index is what
decides whether this voter is allowed through at all. Incrementing first would
double-count anyone who retried a rejected request.

After the insert, **nothing is allowed to turn a successful vote into an
error.** If the Redis increment fails, or the publish fails, it is logged and
the request still succeeds. The voter did nothing wrong, and their vote is
durable. Live viewers miss at most one update — the next event carries a full
snapshot, and a reconnecting client refetches over REST.

### WebSocket fan-out, and why Redis is load-bearing

Each backend instance runs a **hub**: a map of poll ID → the sockets *this
process* is holding. That alone is enough on one machine, and breaks the moment
you run two.

With two instances behind a load balancer, a voter might hit instance A while
the people watching are connected to instance B. An in-process hub means B
never hears about the vote and half the audience watches a frozen screen.

So the vote path publishes to Redis, and every instance subscribes. A vote on A
reaches viewers on B by going out through Redis and back. The hub subscribes to
a poll's channel only when its first local viewer arrives and unsubscribes when
the last one leaves, so an instance receives traffic only for polls it is
actually serving.

**This was verified, not assumed:** two backend instances on 8080 and 8081, a
browser connected to 8080, a vote sent to 8081 — which held zero sockets. The
browser updated. The only path between them is Redis.

### Reconnection

The client reconnects with exponential backoff **plus jitter**. The jitter is
the part that is easy to skip and painful to skip: without it, every client
that dropped when a server restarted reconnects on the same millisecond, and
the stampede knocks the server over again.

On *re*connect — never the first connect — the client refetches over REST.
While a socket was down, votes may have been cast that this client never heard
about. Trusting the screen after a drop is exactly how a "live" page ends up
confidently showing stale numbers.

---

## Key decisions and trade-offs

Written plainly, including the parts I am not fully happy with.

### One document per ballot, not per selection

A vote document holds an array, `optionIndexes`. In single-choice mode it has
one entry; in multiple-choice it has several.

The alternative was one document per selected option with a unique index on
`(pollId, voterKey, optionIndex)`. I rejected it because it quietly breaks
single-choice polls: voting for option 0 and then option 1 produces two
*different* index keys, so both inserts succeed. Catching that needs an
application-level "have they voted already?" check, which reintroduces the
exact check-then-insert race the unique index exists to remove.

With one document per ballot the index stays `(pollId, voterKey)` and the
database enforces one ballot per voter in both modes, with no application check
anywhere.

### Counts are cached in two places, and MongoDB decides

The poll document carries a denormalised `counts` array, and Redis carries a
hash. Both are caches. `RecountByPoll` rebuilds either of them from the votes
collection.

The honest gap: inserting the vote and incrementing the tally are two
operations, not one transaction. If the process dies between them, the count is
low by one while the vote itself is safely stored. I ordered it so the failure
mode is *undercounting*, never a phantom vote, and a recount fixes it. A
MongoDB transaction would close this properly and Atlas supports it — I left it
out because it costs latency on every single vote to defend against a rare
failure that is already repairable. I would revisit that if this were handling
anything that mattered financially.

### Duplicate-vote prevention, and what it does not do

The server issues an HttpOnly cookie with a random 128-bit voter key. A unique
index on `(pollId, voterKey)` means the *database* rejects the second vote — not
application code, so there is no race.

**It stops accidents and casual double-voting. It is not a security control
and I am not presenting it as one.** Clearing cookies, opening a private window
or switching browsers all produce a fresh identity.

Why not the alternatives:

- **localStorage** — page JavaScript can rewrite it in one line from the
  console. The HttpOnly cookie at least cannot be touched by script.
- **IP address** — blocks real voters. Everyone in a lecture hall, an office or
  on one mobile carrier shares an address, and a classroom poll is exactly what
  this is for. I used IP for *rate limiting* instead, which throttles without
  blocking.
- **Requiring login to vote** — genuinely effective, and it would break the
  requirement that anyone with the link can vote.

Raw IPs are never stored; only a salted SHA-256 digest.

### Token in a header, not a cookie

The JWT travels in `Authorization: Bearer`. A browser attaches cookies to
cross-site requests automatically but never attaches an Authorization header
that an attacker's page did not set, so authenticated routes are CSRF-immune by
construction instead of needing CSRF tokens.

It is signed HS256 and parsed with the algorithm **pinned**. Without that pin, a
forged token claiming `alg: none` would be honoured — the algorithm has to be
the server's decision, not the token's.

The trade-off is that the token lives in `localStorage`, which any script on
the page can read, so a successful XSS steals the session. I accepted that
because the app never uses `dangerouslySetInnerHTML` and React escapes output by
default. An HttpOnly cookie would be stronger against XSS and weaker against
CSRF; there is no option that is simply better.

### Validation lives in services, not handlers

Handlers parse JSON and nothing else. Every bound, trim and duplicate check
sits in `internal/services`, so the rules are identical regardless of what calls
them.

Lengths are counted in **runes, not bytes**, so a question in Tamil or Hindi
gets the same 280-character allowance as one in English. The exception is the
password: bcrypt silently ignores everything past 72 *bytes*, so anything longer
is rejected rather than truncated — otherwise two different passwords could open
the same account.

The client mirrors these rules for instant feedback and the server re-checks
everything. The duplication is deliberate; a shared schema would mean a build
step and a code generator for nine numbers.

### Things I would fix with more time

- **No automated test for the realtime path.** I verified it by hand with two
  instances and two browsers, which is convincing but not repeatable. A Go test
  that stands up a hub against a real Redis and asserts a published event
  reaches a subscribed client is the obvious gap.
- **No MongoDB transaction** around vote-insert plus tally-increment, as above.
- **No refresh tokens.** A 24-hour access token and then you log in again.
  Fine here, irritating in a real product.
- **Rate limiting is per-IP only.** Behind CGNAT a whole neighbourhood shares a
  bucket. A per-voter-cookie limit layered on top would be fairer.
- **The counter cache can drift** if two instances seed a missing key at the
  same moment. It self-corrects on the next read after a rebuild, and the
  numbers people *see* come from MongoDB's atomic `$inc`, but it is a real
  window and I would close it with a short lock.
- **No structured frontend tests.** The backend has a test suite; the React
  side has none.

---

## Deployment

See **[DEPLOY.md](DEPLOY.md)** for step-by-step instructions, the environment
variables each service needs, and the CORS / `wss://` / SameSite problems you
will hit.
