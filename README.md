# Live Polls

Create a poll, share the link, and watch results update live as an anonymous
audience votes.

**Status: Phase 2 complete** — Go/Gin backend with MongoDB and authentication,
plus a React frontend that drives the whole flow over plain HTTP.
Redis and WebSocket push arrive in later phases; results currently update on
load and on an explicit refresh.

## Stack

| Layer     | Choice                | Doing what                                          |
| --------- | --------------------- | --------------------------------------------------- |
| Backend   | Go 1.22+ with Gin     | HTTP API, validation, auth                           |
| Database  | MongoDB               | Source of truth: users, polls, votes                 |
| Realtime  | Redis *(phase 4–5)*   | Live counters and pub/sub fan-out across instances   |
| Frontend  | React 19 + Vite       | Auth, poll creation, share links, voting, results    |

## Layout

```
backend/
  cmd/server/          main() — config, wiring, graceful shutdown. Nothing else.
  internal/
    config/            every env var, read and validated in one place
    apperr/            the one error type that crosses layer boundaries
    models/            structs + BSON/JSON tags + the API view types
    db/                the only package that imports the mongo driver
    services/          all business rules and validation
    handlers/          HTTP in, JSON out. No rules, no queries.
    middleware/        request ID, logging, recovery, CORS, body limit, auth

frontend/
  src/
    api/               the only code that calls fetch
    auth/              session context, the useAuth hook, route guard
    hooks/             useAsyncData: load / error / refetch / cancel
    lib/               validation mirrors and formatting
    components/        small, presentational, no network access
    pages/             one per route; all data fetching happens here
```

The dependency direction is one-way: `handlers → services → db → models`.
A handler cannot reach a collection, and a service has no idea HTTP exists.
That is what makes the rules testable without a web server and reusable from
the WebSocket layer in a later phase.

## Running it locally

### 1. Get a MongoDB

Any MongoDB works. The quickest zero-install route is a free
**MongoDB Atlas** cluster:

1. Create a free M0 cluster at `mongodb.com/cloud/atlas`.
2. Database Access → add a user with a password.
3. Network Access → add your current IP (or `0.0.0.0/0` while developing).
4. Connect → Drivers → copy the `mongodb+srv://...` string.

### 2. Configure

```bash
cd backend
cp .env.example .env
```

Fill in `MONGODB_URI` and generate a `JWT_SECRET`:

```bash
openssl rand -base64 48
```

`.env` is gitignored. It never gets committed, and in production these are
real environment variables rather than a file.

### 3. Run

```bash
cd backend
go mod tidy
go run ./cmd/server
```

Startup connects to MongoDB, pings it, and creates indexes before it binds a
port — so a bad connection string fails immediately and loudly instead of on
the first request.

```bash
curl http://localhost:8080/healthz
```

### 4. Run the frontend

In a second terminal:

```bash
cd frontend
cp .env.example .env
npm install
npm run dev
```

Open http://localhost:5173. `VITE_API_BASE_URL` in `frontend/.env` points at
the backend; anything prefixed `VITE_` is compiled into the bundle and is
therefore public, so no secret ever goes in that file.

There is deliberately **no Vite dev proxy**. Proxying `/api` would make the two
halves same-origin in development and hide every CORS and cookie problem until
deployment day. Talking to the real backend origin from the start means dev and
production exercise the same path.

## Key decisions

### Votes are separate documents, counts are a cache

Each vote is its own document in `votes`, never an array pushed onto the poll.
Three reasons:

- A MongoDB document is capped at 16MB, which would cap how many votes a poll
  could ever receive.
- Every vote would rewrite the same document, serialising writes behind one
  lock. Separate documents let concurrent votes proceed independently.
- A unique index across documents can enforce "one vote per voter"; the same
  guarantee inside an array is not available.

The poll document *also* carries a denormalised `counts` array, incremented
with `$inc` on each vote. This is a **cache, not the truth**. Reading results
is then a single document fetch rather than an aggregation over every vote,
which is the difference between a constant-time read and one that degrades as
a poll gets popular.

Because it is only a cache, it can always be rebuilt — `RecountByPoll` does
exactly that, and the read path repairs the cache automatically if its shape
ever stops matching the option list.

The honest tradeoff: inserting the vote and incrementing the counter are two
operations, not one transaction. If the process dies between them, the count
is low by one while the vote itself is safely stored. The vote is written
first deliberately, so the failure mode is *undercounting*, never phantom
votes. A recount fixes it. Wrapping both in a MongoDB transaction is the
proper fix and is available on Atlas; it was left out here because it costs
latency on every single vote to defend against a rare failure that is already
repairable.

### One ballot document, not one document per selection

A poll is single-choice or multiple-choice, chosen by its creator at creation
time and immutable afterwards — switching mid-poll would make ballots cast
before the switch mean something different from those cast after.

Supporting both modes could have meant one vote document per selected option,
with a unique index on `(pollId, voterKey, optionIndex)`. That was rejected: it
would let someone vote for option 0 and then option 1 on a *single*-choice
poll, because each insert is unique. Preventing that would need an
application-level "have they voted already?" check, which reintroduces exactly
the check-then-insert race the index exists to eliminate.

Instead one document is a **ballot**: `optionIndexes` is an array holding one
entry in single mode and one or more in multiple mode. The unique index stays
`(pollId, voterKey)` and enforces one ballot per voter in both modes, with no
application-level check anywhere.

This is also why the API reports two totals. `totalVotes` counts people and is
the denominator for every percentage; `totalSelections` counts choices. They
are equal in single mode. In multiple mode percentages can legitimately sum
past 100%, because "60% of voters chose Go" is the useful statement and
dividing by selections would quietly turn it into something else.

### Duplicate-vote prevention, and what it does not do

When someone votes, the server issues an HttpOnly cookie holding a random
128-bit voter key. A unique index on `(pollId, voterKey)` means the database
itself rejects a second vote — not application code, so there is no race
between checking and inserting.

**This stops accidents and casual double-voting. It is not a security
control, and it is not presented as one.** Clearing cookies, opening a private
window, or switching browsers all produce a fresh identity.

Why this and not the alternatives:

- **localStorage** — page JavaScript can rewrite it in one line from the
  console. The HttpOnly cookie at least cannot be touched by script.
- **IP address** — blocks real voters. Everyone in a lecture hall, an office,
  or on the same mobile carrier shares an address, and a classroom poll is
  exactly the use case this app is for.
- **Requiring login to vote** — would be genuinely effective, and would break
  the brief's requirement that anyone with the link can vote.

An IP-based *rate limit* (as opposed to a hard block) is planned for the Redis
phase, where it belongs. Raw IPs are never stored — only a salted SHA-256
digest.

### Auth token in a header, not a cookie

The JWT travels in `Authorization: Bearer`. A browser attaches cookies to
cross-site requests automatically but never attaches an Authorization header
an attacker's page did not set, so authenticated routes are CSRF-immune by
construction rather than by adding CSRF tokens.

The token is signed HS256 and parsed with `jwt.WithValidMethods` pinned to
HS256. Without that pin, a forged token claiming `alg: none` would be honoured
— the algorithm has to be the server's decision, not the token's.

### Validation lives in services, not handlers

Handlers parse JSON and nothing more. Every bound, every trim, every duplicate
check sits in `internal/services`, so the same rules apply no matter what calls
them — an HTTP request today, a WebSocket message in phase 5.

Lengths are counted in **runes, not bytes**, so a question in Tamil or Hindi
gets the same 280-character allowance as one in English. The one exception is
the password: bcrypt silently ignores everything past 72 *bytes*, so anything
longer is rejected rather than truncated — otherwise two different passwords
could open the same account.

### A cancelled request is not a server error

When someone navigates away mid-request — or React aborts a fetch on unmount,
which StrictMode does on every mount in development — the connection closes,
Gin's request context cancels, and every database call still in flight fails
with `context canceled`.

Classified naively, those are 500s. The user never sees them (they are already
gone), but the server logs a server error for entirely normal client
behaviour. In production that means alerting on healthy traffic and burying
the failures that matter.

So `fail()` checks `c.Request.Context().Err()` first. If the caller has hung
up, the request is logged at debug and closed with **499** — nginx's
non-standard "client closed request". No response body is written, because
there is nobody left to read it. This took the error count during a normal
browsing session from eight 500s to zero.

### Errors are typed and never leak

Services return `*apperr.Error`; handlers turn it into a status code and a
fixed JSON envelope:

```json
{ "error": { "code": "VALIDATION_FAILED", "message": "...", "fields": { "options": "..." }, "requestId": "a1b2c3" } }
```

Anything unclassified becomes a generic 500 with the real cause written only to
the server log. A raw driver error reaching a browser would disclose index
names, collection names and sometimes parts of the connection string.

## Dependencies

Five, each earning its place:

| Module                          | Why                                                          |
| ------------------------------- | ------------------------------------------------------------ |
| `gin-gonic/gin`                 | Required by the brief. Routing, grouping, JSON binding.       |
| `go.mongodb.org/mongo-driver/v2`| Official MongoDB driver. Required by the brief.               |
| `golang-jwt/jwt/v5`             | Hand-rolling JWT parsing is how algorithm-confusion bugs happen. |
| `golang.org/x/crypto`           | bcrypt. Password hashing is never hand-rolled.                |
| `joho/godotenv`                 | Loads `.env` in local development only; real environments use real env vars. |

Logging uses `log/slog` and CORS is ~40 lines written by hand, both to avoid
dependencies that would not have paid for themselves.

## API

All responses are JSON. Errors use the envelope shown above.

| Method | Path                     | Auth   | Purpose                            |
| ------ | ------------------------ | ------ | ---------------------------------- |
| GET    | `/healthz`               | –      | Liveness + a real MongoDB ping     |
| POST   | `/api/auth/signup`       | –      | Create an account, returns a token |
| POST   | `/api/auth/login`        | –      | Exchange credentials for a token   |
| GET    | `/api/auth/me`           | bearer | The account behind the token       |
| POST   | `/api/polls`             | bearer | Create a poll (2–10 options)       |
| GET    | `/api/polls`             | bearer | The caller's own polls             |
| GET    | `/api/polls/:id`         | –      | A poll and its current results     |
| PATCH  | `/api/polls/:id/close`   | owner  | Stop accepting votes               |
| DELETE | `/api/polls/:id`         | owner  | Delete a poll                      |
| POST   | `/api/polls/:id/vote`    | –      | Cast one vote                      |

`:id` accepts either the poll's ObjectID or its share slug. Slugs are random
rather than sequential, so nobody can walk the poll list by editing a URL.

Create takes an optional `"mode": "single" | "multiple"` (defaults to
`single`). A ballot is always a list, in both modes:

```json
POST /api/polls/:id/vote
{ "optionIndexes": [0, 2] }
```

## Frontend design decisions

### One accent, a fixed spacing scale, and nothing else

Every colour, space and radius is a CSS custom property declared once in
`src/index.css`. Components never write a raw hex value, which is why dark mode
is about twenty lines rather than a second stylesheet.

**Accent: a single indigo (`#4f46e5`).** It means exactly one thing — "this is
interactive, or this is a measured quantity". Buttons, focus rings and result
bars share it; nothing decorative is ever accent-coloured. A second accent
would have to earn its meaning, and nothing here needs one.

**Spacing: a 4px scale** (4/8/12/16/24/32/48/64) exposed as `--s1`…`--s8`.
Using only these values is the single cheapest thing that makes unrelated
screens feel like the same product.

**Type: six sizes, system font stack.** A webfont would cost a round trip and a
layout shift to buy very little. Headings are tightened (`-0.02em`) because
default tracking looks loose at large sizes; body copy sits at 1.55 line-height
for readability. Content is capped at a 40rem measure — long lines are the most
common readability failure in hand-rolled layouts.

**Dark mode is selected, not inverted.** Each dark value is chosen against the
dark surface: the accent *lightens* (a 600-weight indigo fails contrast on
near-black), borders lift rather than darken, and the page background is a
desaturated navy rather than pure black, which is easier on the eyes at night.

**Responsive** down to 375px: one column throughout, 16px side gutters, and
every control at least 44px tall for touch.

### The results chart

One series — this poll — so every bar wears the same accent. Giving each option
its own hue would imply the colours carry meaning they do not, and makes the
chart harder to read rather than easier.

- Identity comes from the text label above each bar, never from colour.
- Every bar is directly labelled with its count and percentage. With at most
  ten rows, the numbers *are* the content; hiding them in a hover tooltip would
  put the primary information out of reach on touch devices.
- The leading option is emphasised with **font weight, not colour**, so it
  survives greyscale and colour-vision differences.
- Bars are thin (10px) with a rounded data-end and a square baseline end, so
  they read as anchored to zero.
- A zero-vote bar draws nothing rather than a sliver, so empty is honestly
  empty.
- Each track carries an `aria-label` with the same numbers, so a screen reader
  gets the value directly instead of interpreting a decorative div.

### Client validation mirrors the server, and never replaces it

`src/lib/validation.js` duplicates the bounds from
`backend/internal/services/validate.go` so the form can respond instantly.
Anyone can skip it entirely with curl, so the server re-checks everything on
arrival. The duplication is deliberate: a shared schema would mean a build step
and a code generator for nine numbers.

Character counts use `[...str].length` rather than `.length`, which counts
characters instead of UTF-16 code units — the same way the Go side counts runes.

### Data fetching lives at page level

Pages call the API; components receive data and callbacks. `useAsyncData`
handles loading, error, refetch and cancellation in one place, so a stray
setState-after-unmount or an unhandled rejection can only be written once. It
passes an `AbortSignal` into every request, so navigating away mid-flight
actually cancels the request rather than ignoring its result.

No data-fetching library. TanStack Query would add caching and deduplication,
but with three read screens and no shared cache to speak of, the dependency
would cost more to explain than it saves.

### Token storage, honestly

The JWT lives in `localStorage`, which any script on the page can read — a
successful XSS steals the session. The airtight alternative is an HttpOnly
cookie, but that is attached to every request automatically and reintroduces
CSRF. Since the API takes the token in an `Authorization` header (making
authenticated routes CSRF-proof by construction), `localStorage` is the
deliberate trade, backed by React escaping all output and no use of
`dangerouslySetInnerHTML` anywhere.

The stored token is only a *claim*: on boot it is verified against
`/api/auth/me` before the app treats anyone as signed in, because it may have
expired while the tab was closed. Session state is three-valued
(`loading` / `authenticated` / `anonymous`) rather than a boolean, so a refresh
on a protected page does not flash the login screen for a frame.

### No `alert()`

Errors render as an `Alert` with `role="alert"`, so they are announced rather
than merely coloured. The one `window.confirm` is on poll deletion — a genuinely
irreversible action, where a hand-rolled modal would be more code for worse
accessibility. That one asks a question and uses the answer.

## Frontend dependencies

Three, plus the build tool:

| Package            | Why                                                        |
| ------------------ | ---------------------------------------------------------- |
| `react`, `react-dom` | Required by the brief.                                   |
| `react-router-dom` | Six routes with public/protected split and URL params.     |
| `vite`, `@vitejs/plugin-react` | Build tool and JSX transform.                  |

No UI kit, no CSS framework, no state library, no form library, no HTTP client.
`fetch` is built in; the styling is ~400 lines of CSS that can be read end to
end; and the app's state is small enough that `useState` plus one context is
the right answer rather than a compromise.

## Tests

```bash
cd backend
go test ./...
```

Covers route registration, that every protected route rejects anonymous
callers, the CORS allowlist, and token rejection. These need no database.
