#!/bin/bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd)
IMAGE_NAME=${IMAGE_NAME:-qywx-x86-ubuntu2404-builder}

if ! docker image inspect "$IMAGE_NAME" >/dev/null 2>&1; then
  echo "[info] 构建 x86 构建容器镜像 $IMAGE_NAME (Ubuntu 24.04)"
  docker build --platform=linux/amd64 -t "$IMAGE_NAME" "$SCRIPT_DIR"
fi

docker run --rm --platform=linux/amd64 \
  -v "$ROOT_DIR":/workspace \
  -w /workspace \
  "$IMAGE_NAME" bash -c "make clean && make debug"
