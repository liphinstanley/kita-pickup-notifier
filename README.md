# kita-pickup-notifier

Weekday push notification telling you when to leave the house for kita pickup, based on live BVG (Berlin) departures.

```
Cloud Scheduler (*/5 14 * * 1-5, Europe/Berlin)
   └─▶ Cloud Run job (Go, stateless, ~3 s)
         1. skip weekends, Berlin public holidays, SKIP_DATES
         2. already pushed today? (asks ntfy's 12 h message cache)
         3. v6.bvg.transport.rest /journeys arriving by ARRIVE_BY - BUFFER_MIN
         4. pick the latest-leaving trip (no walk-only, no cancelled legs)
         5. if now >= leave - LEAD_MIN → push via ntfy.sh
```

If BVG is down and `FALLBACK_LEAVE` is set, a generic "leave at HH:MM" push goes out instead.

## Phone
Install **ntfy** (iOS App Store / Google Play) → `+` → subscribe to the topic in `.env` (server ntfy.sh). Tapping the push opens Google Maps transit directions.

## Run locally
```sh
set -a; source .env; set +a
DRY_RUN=1 NOW=2026-11-16T14:50 go run .
go test ./...
```

## Deploy
`cp .env.example .env`, fill in, then `./deploy.sh` (personal gcloud login). Change times or closing days → edit `.env`, re-run `./deploy.sh`.

## Cost
Free tier: 1 of 3 free Cloud Scheduler jobs, ~250 job runs/month (well under 180k vCPU-s), ~10 MB image (AR keeps last 2), ntfy.sh and transport.rest are free.
