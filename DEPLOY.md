# Deploying Live Polls

Four services, all on free tiers:

| Piece | Where | Why this one |
| --- | --- | --- |
| Backend (Go) | **Render** | Free tier supports WebSockets, builds from a Dockerfile, no card required |
| Frontend (React) | **Vercel** | Free, builds from the repo on push, global CDN |
| Database | **MongoDB Atlas** | Free M0 cluster |
| Redis | **Upstash** | Free tier, TLS by default, no card required |

Alternatives that also work: **Railway** or **Fly.io** for the backend (both
support WebSockets), **Netlify** for the frontend. Avoid anything that only
offers serverless functions for the backend — a WebSocket needs a process that
stays alive, and a Lambda-style function cannot hold one.

## One-click deploy

These read `render.yaml` and `vercel.json` from the repo and pre-fill the
forms, so the only manual work left is signing in and pasting the secret
values.

**Backend (Render)** — reads `render.yaml`, prompts for the five `sync: false`
secrets:

https://render.com/deploy?repo=https%3A%2F%2Fgithub.com%2FMonashini%2Flive-polls

**Frontend (Vercel)** — sets root directory to `frontend` and prompts for
`VITE_API_BASE_URL`:

https://vercel.com/new/clone?repository-url=https%3A%2F%2Fgithub.com%2FMonashini%2Flive-polls&root-directory=frontend&project-name=live-polls&env=VITE_API_BASE_URL&envDescription=Your%20Render%20backend%20origin%2C%20https%2C%20no%20trailing%20slash&envLink=https%3A%2F%2Fgithub.com%2FMonashini%2Flive-polls%2Fblob%2Fmain%2FDEPLOY.md

Deploy the backend first so you have its URL for `VITE_API_BASE_URL`, then come
back and set `CORS_ALLOWED_ORIGINS` on Render to the Vercel URL.

---

## Before you start

You already have MongoDB Atlas and Upstash from local development. Two things
need changing for production:

**Atlas → Network Access → add `0.0.0.0/0`.** Render's free tier has no static
outbound IP, so there is no single address to allow. If this feels wrong: the
database still requires username and password, the allowlist is a second layer,
and the free tier gives you no alternative. A paid Render plan offers static
IPs if you want to tighten it later.

**Upstash → copy the `rediss://` URL**, not the REST one. The app speaks the
Redis wire protocol, not Upstash's HTTP API.

---

## Step 1 — Backend on Render

1. Push your code to GitHub (already done).
2. **render.com** → New → **Web Service** → connect the `live-polls` repo.
3. Settings:
   - **Root Directory**: `backend`
   - **Runtime**: `Docker`
   - **Instance Type**: Free
4. Add the environment variables below, then **Create Web Service**.

### Backend environment variables

| Variable | Value | Notes |
| --- | --- | --- |
| `APP_ENV` | `production` | Switches Gin to release mode and defaults cookies to Secure |
| `MONGODB_URI` | your Atlas string | Same one as local |
| `MONGODB_DATABASE` | `livepolls` | |
| `REDIS_URL` | your Upstash `rediss://` string | **Two esses.** One `s` fails against a TLS host |
| `JWT_SECRET` | `openssl rand -base64 48` | **Generate a new one.** Never reuse your local secret |
| `JWT_TTL` | `24h` | |
| `IP_HASH_SALT` | `openssl rand -base64 32` | Generate a new one |
| `CORS_ALLOWED_ORIGINS` | `https://your-app.vercel.app` | Exact. No trailing slash. See gotchas |
| `COOKIE_SECURE` | `true` | |
| `COOKIE_SAMESITE` | `none` | **Required.** See gotchas |
| `TRUSTED_PROXIES` | `0.0.0.0/0` | **Required.** See gotchas |

Do **not** set `PORT` — Render provides it and `config.go` already reads it.

You will not know your Vercel URL yet. Put a placeholder in
`CORS_ALLOWED_ORIGINS`, deploy the frontend, then come back and correct it.

Check it worked:

```bash
curl https://your-service.onrender.com/healthz
```

Expect `{"mongo":"ok","redis":"ok","status":"ok",...}`. If `redis` says
`unreachable`, your URL is almost certainly `redis://` instead of `rediss://`.

---

## Step 2 — Frontend on Vercel

1. **vercel.com** → Add New → Project → import the same repo.
2. Settings:
   - **Root Directory**: `frontend`
   - **Framework Preset**: Vite
   - Build command and output directory are detected automatically.
3. Environment variable:

| Variable | Value |
| --- | --- |
| `VITE_API_BASE_URL` | `https://your-service.onrender.com` |

No trailing slash. It must be `https`, not `http` — see the `wss://` gotcha.

4. Deploy, copy the resulting URL, and go back to Render to put it in
   `CORS_ALLOWED_ORIGINS`. Render redeploys on save.

---

## The gotchas you will actually hit

These are in the order you are likely to meet them.

### 1. Client-side routes 404 on refresh

Open `https://your-app.vercel.app/p/abc123`, press F5, get a 404. The share
link — the whole product — appears broken.

Vercel looks for a file at `/p/abc123`. There isn't one; React Router resolves
that path in the browser. Every path has to be served `index.html` so the app
can boot and route.

`frontend/vercel.json` in this repo already does that. If you deploy somewhere
else, you need the equivalent rewrite rule.

### 2. `wss://` and mixed content

A page served over `https` may not open a `ws://` socket. The browser blocks it
as mixed content and the live feed silently never connects — everything else
keeps working, which makes it confusing to diagnose.

`socketURL()` in `api/client.js` derives the socket URL from
`VITE_API_BASE_URL` by swapping `http` → `ws`. So `https://` becomes `wss://`
automatically **as long as you set the base URL to https**. Setting it to
`http://` is what breaks this, and it breaks only the sockets.

### 3. The voter cookie will not be sent — SameSite

This is the subtle one, and it silently disables duplicate-vote prevention.

Locally, frontend and backend are both `localhost`, so the voter cookie is
same-site and a default `SameSite=Lax` works. In production
`your-app.vercel.app` and `your-service.onrender.com` are **different sites**.
A `Lax` cookie is not sent on a cross-site `fetch`, so the server sees no voter
cookie, issues a new one on every vote, and everybody can vote unlimited times.

Fix: `COOKIE_SAMESITE=none` and `COOKIE_SECURE=true`. Browsers reject
`SameSite=None` without `Secure`, so both are required together — `config.go`
refuses to start if you set one without the other.

### 4. CORS — exact origins, and preview deployments

`CORS_ALLOWED_ORIGINS` must match the browser's `Origin` header exactly:
scheme, host, no trailing slash, no path. `https://your-app.vercel.app/` with
a trailing slash will not match.

The non-obvious part: **every Vercel preview deployment gets its own URL**
(`your-app-git-branch-you.vercel.app`). Those will fail CORS. Either add them
to the list as needed, or test against the production URL.

Wildcards are rejected at startup, deliberately: the voter cookie makes every
vote a credentialed request, and browsers refuse to send credentials to a
wildcard origin. `*` would look like it worked and quietly break voting.

### 5. Rate limiting will throttle everyone at once — trusted proxies

Render terminates TLS at its own proxy and forwards to your container. Without
`TRUSTED_PROXIES`, Gin ignores `X-Forwarded-For` and reports the *proxy's*
address as the client IP for every request. Every visitor then shares one
rate-limit bucket, and after 30 votes the entire site starts returning 429.

Setting `TRUSTED_PROXIES=0.0.0.0/0` tells Gin to read the real client IP from
the forwarded header.

Why `0.0.0.0/0` and not a private range: Render's edge runs on public cloud
addresses, so `10.0.0.0/8` would not match and you would get exactly the
failure above while believing you had fixed it. The cost of the broader
setting is that someone can forge `X-Forwarded-For` to dodge their own rate
limit — acceptable here, because this limit is load protection rather than an
authorisation control, and double voting is stopped by a unique index keyed on
a cookie, not by IP. If you ever gate something important on client IP, replace
this with your platform's real egress range.

### 6. Render's free tier sleeps

A free service spins down after ~15 minutes of inactivity and takes **30–60
seconds** to wake. For a submission where a reviewer clicks your link cold,
that is a bad first impression, and the WebSocket will fail its first connect
attempt before the reconnect backoff succeeds.

Options, in order of how much I would trust them:

- **Upgrade to the $7/month Starter plan** while your application is being
  reviewed. Cheapest way to be certain the link works.
- Ping `/healthz` every 10 minutes from a free uptime monitor
  (cron-job.org, UptimeRobot). Keeps it warm, though Render may still cycle it.
- Accept it and warn the reviewer in your email that the first load takes a
  minute. Honest, and less risky than a link that appears dead.

### 7. Cold starts and `/healthz`

Point Render's health check at `/healthz`. It pings MongoDB for real, so a
container that started but cannot reach the database is correctly reported as
unhealthy instead of silently serving errors.

---

## Verifying the deployment

```bash
curl https://your-service.onrender.com/healthz
```

Then in a browser, with two windows side by side on the deployed URL:

1. Sign up, create a poll, copy the share link.
2. Open the link in a second window (or a phone).
3. Vote in one. The other must move without a refresh.
4. Open DevTools → Network → WS. You should see a `101 Switching Protocols`
   on `wss://your-service.onrender.com/ws/polls/...`, and frames arriving as
   votes are cast.
5. Vote twice from the same browser — the second must be rejected.

If step 3 fails but everything else works, it is almost always gotcha 2 or 4.
If step 5 fails — you can vote repeatedly — it is gotcha 3.
