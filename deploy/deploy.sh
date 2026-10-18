#!/bin/sh
# Runs on the relay server as root, from CI: ssh root@host 'sh -s -- <command>' < deploy/deploy.sh
# Not stored on the server. The project lives in /opt/gate4ai-www: compose.yaml (uploaded
# by CI), .env with WWW_TAG (the image tag, written here), www.env (secrets, by hand).
#
#   up <tag>   pull ghcr.io/gate4ai/www:<tag> and start it; keep the previous tag in .env.prev.
#              If http://127.0.0.1:8080/healthz does not answer, start the previous tag again.
#   rollback   start the previous tag again (CI: https://gate4.ai/healthz does not answer).
#
# The registry credentials for the pull come on stdin of `docker login` before this script
# (CI logs in with its job token and logs out after the deploy).
set -eu

DIR=/opt/gate4ai-www
cd "$DIR"

health() {
  i=0
  while [ $i -lt 30 ]; do
    if curl -fsS --max-time 5 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then return 0; fi
    i=$((i + 1))
    sleep 1
  done
  return 1
}

start() { # start <tag>
  printf 'WWW_TAG=%s\n' "$1" >.env.tmp && mv -f .env.tmp .env
  docker compose pull -q
  docker compose up -d --remove-orphans
}

rollback() {
  if [ ! -f .env.prev ]; then
    echo "gate4ai-www: no previous tag to roll back to" >&2
    return 1
  fi
  prev=$(sed -n 's/^WWW_TAG=//p' .env.prev)
  echo "gate4ai-www: rolling back to $prev" >&2
  start "$prev"
  rm -f .env.prev
  health || { echo "gate4ai-www: $prev is not healthy either" >&2; return 1; }
}

case "${1:-}" in
up)
  tag=${2:?usage: deploy.sh up <tag>}
  if [ -f .env ] && ! grep -qx "WWW_TAG=$tag" .env; then cp -p .env .env.prev; fi
  start "$tag"
  if health; then
    echo "gate4ai-www: $tag, healthy"
  else
    echo "gate4ai-www: $tag: health check failed" >&2
    docker compose logs --tail 30 >&2 || true
    rollback || true
    exit 1
  fi
  docker image prune -f >/dev/null
  ;;
rollback) rollback ;;
*)
  echo "usage: deploy.sh up <tag> | rollback" >&2
  exit 2
  ;;
esac
