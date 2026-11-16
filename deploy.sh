#!/usr/bin/env bash
# Idempotent deploy: project + APIs + Artifact Registry + Cloud Run job + Cloud Scheduler.
# Everything stays inside the GCP free tier (1 scheduler job, ~250 short job runs/month, ~10 MB image).
set -euo pipefail
cd "$(dirname "$0")"
set -a; source .env; set +a

JOB=kita-pickup
REPO=kita-pickup
IMAGE="$REGION-docker.pkg.dev/$PROJECT/$REPO/notifier:$(git rev-parse --short HEAD 2>/dev/null || date +%Y%m%d%H%M%S)"
RUN_SA="notifier-run@$PROJECT.iam.gserviceaccount.com"
SCHED_SA="scheduler-invoker@$PROJECT.iam.gserviceaccount.com"
SCHEDULE="${SCHEDULE:-*/5 14 * * 1-5}"   # every 5 min, 14:00-14:55, Mon-Fri, Europe/Berlin
g() { gcloud --project "$PROJECT" --quiet "$@"; }

echo "== project"
gcloud projects describe "$PROJECT" >/dev/null 2>&1 || gcloud projects create "$PROJECT" --name "kita-pickup"
gcloud billing projects link "$PROJECT" --billing-account "$BILLING_ACCOUNT" >/dev/null
g services enable run.googleapis.com cloudscheduler.googleapis.com artifactregistry.googleapis.com

echo "== artifact registry"
if ! g artifacts repositories describe "$REPO" --location "$REGION" >/dev/null 2>&1; then
  g artifacts repositories create "$REPO" --location "$REGION" --repository-format docker
fi
cat > /tmp/kita-ar-policy.json <<'EOF'
[{"name":"keep-latest-2","action":{"type":"Keep"},"mostRecentVersions":{"keepCount":2}},
 {"name":"delete-rest","action":{"type":"Delete"},"condition":{"tagState":"ANY"}}]
EOF
g artifacts repositories set-cleanup-policies "$REPO" --location "$REGION" --policy /tmp/kita-ar-policy.json --no-dry-run >/dev/null

echo "== image $IMAGE"
gcloud auth print-access-token | docker login -u oauth2accesstoken --password-stdin "https://$REGION-docker.pkg.dev" >/dev/null
docker buildx build --platform linux/amd64 -t "$IMAGE" --push .

echo "== service accounts"
for sa in notifier-run scheduler-invoker; do
  g iam service-accounts describe "$sa@$PROJECT.iam.gserviceaccount.com" >/dev/null 2>&1 || g iam service-accounts create "$sa"
done

echo "== cloud run job"
ENVFILE=$(mktemp)
for k in FROM_LAT FROM_LON FROM_NAME TO_LAT TO_LON TO_NAME ARRIVE_BY BUFFER_MIN LEAD_MIN FALLBACK_LEAVE SKIP_DATES NTFY_TOPIC NTFY_TOKEN; do
  printf '%s: "%s"\n' "$k" "${!k:-}" >> "$ENVFILE"
done
g run jobs deploy "$JOB" --region "$REGION" --image "$IMAGE" --service-account "$RUN_SA" \
  --env-vars-file "$ENVFILE" --cpu 1 --memory 512Mi --max-retries 0 --task-timeout 120s
rm -f "$ENVFILE"
g run jobs add-iam-policy-binding "$JOB" --region "$REGION" --member "serviceAccount:$SCHED_SA" --role roles/run.invoker >/dev/null

echo "== scheduler"
URI="https://run.googleapis.com/v2/projects/$PROJECT/locations/$REGION/jobs/$JOB:run"
verb=create
g scheduler jobs describe "$JOB" --location "$REGION" >/dev/null 2>&1 && verb=update
g scheduler jobs "$verb" http "$JOB" --location "$REGION" --schedule "$SCHEDULE" --time-zone Europe/Berlin \
  --uri "$URI" --http-method POST --oauth-service-account-email "$SCHED_SA" \
  --oauth-token-scope https://www.googleapis.com/auth/cloud-platform >/dev/null

echo "done. Subscribe to ntfy topic: $NTFY_TOPIC"
