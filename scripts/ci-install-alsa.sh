#!/usr/bin/env bash
#
# Install the ALSA headers the voice packages build against, without letting a
# stuck apt hang the job.
#
# A plain `apt-get update && apt-get install` on a hosted runner has hung for
# hours (until the job's 6h limit cancelled it): waiting on a dpkg lock held by
# the runner's own unattended upgrades, or on a mirror that accepts the
# connection and never answers. Bound each wait, and retry, since the next
# attempt usually gets a different mirror or a released lock.
set -euo pipefail

APT_OPTS=(
  -o DPkg::Lock::Timeout=60
  -o Acquire::Retries=3
  -o Acquire::http::Timeout=20
  -o Acquire::https::Timeout=20
)

for attempt in 1 2 3; do
  if timeout 120 sudo apt-get "${APT_OPTS[@]}" update &&
     timeout 120 sudo apt-get "${APT_OPTS[@]}" install -y --no-install-recommends libasound2-dev; then
    exit 0
  fi
  echo "::warning::installing libasound2-dev failed (attempt ${attempt}/3); retrying"
  sleep 10
done

echo "::error::could not install libasound2-dev after 3 attempts"
exit 1
