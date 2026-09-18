
# Live Polls

A real-time polling application where users can create polls, share them through a link, and see voting results update instantly without refreshing the page.

## Live Demo

- **Frontend:** https://mona-live-poll.vercel.app
- **Backend:** https://live-polls-kwjq.onrender.com/healthz
- **GitHub:** https://github.com/Monashini/live-polls

> The backend is hosted on Render's free tier. After a period of inactivity, the first request may take around 30–60 seconds while the service wakes up.

## Features

- User signup and login
- Create polls with 2–10 options
- Single-choice and multiple-choice polls
- Shareable poll links
- Anonymous voting
- Duplicate vote prevention
- Real-time result updates without page refresh
- Close or delete polls with owner authorization
- Rate limiting for voting requests
- MongoDB persistence

## Tech Stack

| Part | Technology |
|------|------------|
| Frontend | React + Vite |
| Backend | Go + Gin |
| Database | MongoDB |
| Realtime | Redis + WebSocket |
| Frontend Hosting | Vercel |
| Backend Hosting | Render |
| Redis Hosting | Upstash |

## Architecture

```text
                    React + Vite
                    (Vercel)
                         |
                         v
                   Go + Gin API
                    (Render)
                    /        \
                   /          \
                  v            v
             MongoDB         Redis
             Atlas           Upstash
             |                |
             |                +---- Pub/Sub
             |                +---- Live counters
             |                +---- Rate limiting
             |
             +---- Persistent votes
                         |
                         v
                    WebSocket
                         |
                         v
                Connected browsers
````

### How voting works

1. The user selects an option and submits a vote.
2. The Go/Gin backend validates the request and stores the vote in MongoDB.
3. Redis updates the live counters and publishes an event.
4. The WebSocket connection sends the updated poll results to connected browsers.
5. Other users see the new results without refreshing the page.

MongoDB is the persistent source of truth, while Redis handles the live/realtime layer.

## Project Structure

```text
live-polls/
├── backend/
│   ├── cmd/
│   ├── internal/
│   │   ├── config/
│   │   ├── db/
│   │   ├── handlers/
│   │   ├── middleware/
│   │   ├── models/
│   │   ├── redisstore/
│   │   ├── services/
│   │   └── ws/
│   └── scripts/
│
├── frontend/
│   └── src/
│       ├── api/
│       ├── auth/
│       ├── components/
│       ├── hooks/
│       ├── lib/
│       └── pages/
│
└── DEPLOY.md
```

## Running Locally

### 1. Clone the repository

```bash
git clone https://github.com/Monashini/live-polls.git
cd live-polls
```

### 2. Backend

```bash
cd backend
cp .env.example .env
go run ./cmd/server
```

Configure the required environment variables in `backend/.env`.

Required variables include:

```text
MONGODB_URI=
REDIS_URL=
JWT_SECRET=
CORS_ALLOWED_ORIGINS=
```

### 3. Frontend

Open another terminal:

```bash
cd frontend
npm install
npm run dev
```

Then open:

```text
http://localhost:5173
```

For local development, MongoDB and Redis can either be run locally or replaced with hosted services such as MongoDB Atlas and Upstash Redis.

## API Endpoints

| Method | Endpoint               | Purpose                |
| ------ | ---------------------- | ---------------------- |
| POST   | `/api/auth/signup`     | Create an account      |
| POST   | `/api/auth/login`      | Login                  |
| GET    | `/api/auth/me`         | Get current user       |
| POST   | `/api/polls`           | Create a poll          |
| GET    | `/api/polls`           | Get user's polls       |
| GET    | `/api/polls/:id`       | Get poll and results   |
| POST   | `/api/polls/:id/vote`  | Submit a vote          |
| PATCH  | `/api/polls/:id/close` | Close a poll           |
| DELETE | `/api/polls/:id`       | Delete a poll          |
| GET    | `/ws/polls/:id`        | WebSocket live results |

## Realtime Updates

Redis is used for:

* Live poll counters
* Pub/Sub between backend instances
* Vote rate limiting

WebSocket connections deliver poll updates to connected clients.

This allows a vote made in one browser to appear in another browser without a page refresh.

## Testing

The backend includes tests and an API smoke test.

```bash
cd backend
go test ./...
bash scripts/api-smoke.sh
```

Production verification included:

* 51/51 API assertions passed
* WebSocket `101 Switching Protocols` verified
* WSS connection verified in production
* Two independent browsers received live updates without refresh
* Redis Pub/Sub realtime behavior verified
* MongoDB persistence verified
* Duplicate vote protection verified
* Poll closing updates were delivered in real time

## Deployment

The application is deployed using:

* **Vercel** — React frontend
* **Render** — Go/Gin backend
* **MongoDB Atlas** — persistent database
* **Upstash Redis** — realtime and rate limiting

Detailed deployment instructions are available in [DEPLOY.md](DEPLOY.md).

## Known Limitations

* The realtime path does not currently have a fully automated integration test.
* Vote insertion and tally updates are separate database operations.
* Authentication currently uses access tokens without refresh tokens.
* There is no dedicated frontend test suite yet.

```

