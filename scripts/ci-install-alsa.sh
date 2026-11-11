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
#
# The runner's apt sources point at its cloud's Ubuntu mirror
# (azure.archive.ubuntu.com on GitHub-hosted runners). When that mirror is
# down, every retry against it fails too, so after a failed attempt the
# sources are switched to Ubuntu's main archive.
set -euo pipefail

use_main_archive() {
  local f
  for f in /etc/apt/sources.list /etc/apt/sources.list.d/*.sources /etc/apt/sources.list.d/*.list; do
    [ -f "$f" ] || continue
    sudo sed -i -E 's#https?://[a-z0-9.-]*\.archive\.ubuntu\.com#http://archive.ubuntu.com#g' "$f"
  done
}

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
  echo "::warning::installing libasound2-dev failed (attempt ${attempt}/3); retrying against archive.ubuntu.com"
  use_main_archive
  sleep 10
done

echo "::error::could not install libasound2-dev after 3 attempts"
exit 1
