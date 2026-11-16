# kita-pickup-notifier

Weekday Telegram notification telling you when to leave the house for kita pickup, based on live BVG (Berlin) departures.

```
Cloud Scheduler (*/5 14 * * 1-5, Europe/Berlin)
   └─▶ Cloud Run job (Go, stateless, ~3 s)
         1. skip weekends, Berlin public holidays, SKIP_DATES
         2. already sent today? (date in gs://$STATE_BUCKET/last-sent)
         3. v6.bvg.transport.rest /journeys arriving by ARRIVE_BY - BUFFER_MIN
         4. pick the latest-leaving trip (no walk-only, no cancelled legs)
         5. if now >= leave - LEAD_MIN → Telegram bot message, then record today
```

If BVG is down and `FALLBACK_LEAVE` is set, a generic "leave at HH:MM" push goes out instead.

## Phone
1. Create a bot with @BotFather (`/newbot`), put the token in `.env` as `TELEGRAM_TOKEN`.
2. Each recipient opens the bot in Telegram and taps **Start**.
3. Get their chat IDs: `curl -s https://api.telegram.org/bot$TELEGRAM_TOKEN/getUpdates` → `message.chat.id`, put them in `TELEGRAM_CHAT_IDS`.

Why not ntfy.sh: its free tier limits messages per source IP (even with an account), and Cloud Run's shared egress IPs hit that quota daily (HTTP 429 code 42908).

## Run locally
```sh
set -a; source .env; set +a
DRY_RUN=1 NOW=2026-11-16T14:50 go run .     # plan + render, sends nothing
TEST=1 go run .                              # real test message, no state change
go test ./...
```

## Deploy
`cp .env.example .env`, fill in, then `./deploy.sh` (personal gcloud login). Change times or closing days → edit `.env`, re-run `./deploy.sh`.

## Cost
Free tier: 1 of 3 free Cloud Scheduler jobs, ~250 job runs/month (well under 180k vCPU-s), ~10 MB image (AR keeps last 2), state object in us-central1 (always-free GCS), Telegram Bot API and transport.rest are free.
