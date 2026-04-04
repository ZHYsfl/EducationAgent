#!/usr/bin/env bash
set -euo pipefail

CONTAINER_NAME="education-agent-redis"
IMAGE="redis:7-alpine"
PORT="6379"

usage() {
  cat <<USAGE
Usage: $0 <start|stop|rm|status|logs>

Commands:
  start   Start local Redis container on port ${PORT}
  stop    Stop Redis container
  rm      Remove Redis container
  status  Show Redis container status
  logs    Tail Redis container logs
USAGE
}

ensure_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    echo "docker command not found" >&2
    exit 1
  fi
}

container_exists() {
  docker ps -a --format '{{.Names}}' | grep -Fxq "${CONTAINER_NAME}"
}

start() {
  ensure_docker
  if container_exists; then
    if docker ps --format '{{.Names}}' | grep -Fxq "${CONTAINER_NAME}"; then
      echo "Redis container '${CONTAINER_NAME}' is already running"
      return
    fi
    docker start "${CONTAINER_NAME}" >/dev/null
    echo "Redis container '${CONTAINER_NAME}' started"
    return
  fi

  docker run -d \
    --name "${CONTAINER_NAME}" \
    -p "${PORT}:6379" \
    --restart unless-stopped \
    "${IMAGE}" >/dev/null
  echo "Redis container '${CONTAINER_NAME}' created and started on localhost:${PORT}"
}

stop() {
  ensure_docker
  if ! container_exists; then
    echo "Redis container '${CONTAINER_NAME}' does not exist"
    return
  fi
  docker stop "${CONTAINER_NAME}" >/dev/null
  echo "Redis container '${CONTAINER_NAME}' stopped"
}

rm_container() {
  ensure_docker
  if ! container_exists; then
    echo "Redis container '${CONTAINER_NAME}' does not exist"
    return
  fi
  if docker ps --format '{{.Names}}' | grep -Fxq "${CONTAINER_NAME}"; then
    docker stop "${CONTAINER_NAME}" >/dev/null
  fi
  docker rm "${CONTAINER_NAME}" >/dev/null
  echo "Redis container '${CONTAINER_NAME}' removed"
}

status() {
  ensure_docker
  docker ps -a --filter "name=^/${CONTAINER_NAME}$" --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
}

logs() {
  ensure_docker
  docker logs -f "${CONTAINER_NAME}"
}

main() {
  if [[ $# -ne 1 ]]; then
    usage
    exit 1
  fi

  case "$1" in
    start) start ;;
    stop) stop ;;
    rm) rm_container ;;
    status) status ;;
    logs) logs ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
