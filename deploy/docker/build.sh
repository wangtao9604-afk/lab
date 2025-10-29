#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<USAGE
用法: $(basename "$0") -s <consumer|producer|recorder> [-t <image_name>]
  -s  指定需要构建的微服务
  -t  完整镜像名称(可选，默认: qywx/<service>:latest)
USAGE
}

svc=""
image=""

while getopts ":s:t:h" opt; do
  case "$opt" in
    s)
      svc="$OPTARG"
      ;;
    t)
      image="$OPTARG"
      ;;
    h)
      usage
      exit 0
      ;;
    :)
      echo "参数 -$OPTARG 需要值" >&2
      usage
      exit 1
      ;;
    *)
      usage
      exit 1
      ;;
  esac
done

if [[ -z "$svc" ]]; then
  echo "必须通过 -s 指定要构建的微服务" >&2
  usage
  exit 1
fi

case "$svc" in
  consumer|producer|recorder)
    ;;
  *)
    echo "不支持的服务: $svc" >&2
    usage
    exit 1
    ;;
 esac

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
DOCKERFILE="${SCRIPT_DIR}/Dockerfile.${svc}"

if [[ ! -f "$DOCKERFILE" ]]; then
  echo "未找到 Dockerfile: $DOCKERFILE" >&2
  exit 1
fi

if [[ -z "$image" ]]; then
  image="qywx/${svc}:latest"
fi

PLATFORM="linux/amd64"

echo "构建镜像: $image (平台: ${PLATFORM})"

if docker buildx version >/dev/null 2>&1; then
  docker buildx build \
    --platform "${PLATFORM}" \
    --load \
    -f "$DOCKERFILE" \
    -t "$image" \
    "$PROJECT_ROOT"
else
  docker build \
    --platform "${PLATFORM}" \
    -f "$DOCKERFILE" \
    -t "$image" \
    "$PROJECT_ROOT"
fi
