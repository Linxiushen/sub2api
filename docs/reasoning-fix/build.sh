#!/usr/bin/env bash
set -euo pipefail

# Build only. This script never restarts services or connects to a database.
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
if [[ -z "${SOURCE_DIR:-}" ]]; then
  if [[ -f "$SCRIPT_DIR/../../Dockerfile" ]]; then
    SOURCE_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)"
  elif [[ -f "$SCRIPT_DIR/sub2api/Dockerfile" ]]; then
    SOURCE_DIR="$SCRIPT_DIR/sub2api"
  else
    SOURCE_DIR="$SCRIPT_DIR/../sub2api"
  fi
fi

PLATFORM="${PLATFORM:-linux/amd64}"
IMAGE="sub2api-reasoning-fix:69d2fae-20261118"
BASE_COMMIT="69d2fae91318359e4e25c0171eaf70364dad3629"

if ! command -v docker >/dev/null 2>&1; then
  printf '%s\n' '未找到 docker；请在已有 Docker 环境的构建机器执行。' >&2
  exit 1
fi
if [[ ! -f "$SOURCE_DIR/Dockerfile" || ! -f "$SOURCE_DIR/backend/go.mod" ]]; then
  printf '%s\n' '源码目录无效。请用 SOURCE_DIR 指向本包修复后的 sub2api 源码目录。' >&2
  exit 1
fi
if [[ "$PLATFORM" != "linux/amd64" && "$PLATFORM" != "linux/arm64" ]]; then
  printf '%s\n' 'PLATFORM 仅支持 linux/amd64 或 linux/arm64；请与目标服务主机一致。' >&2
  exit 1
fi

docker info >/dev/null
printf '构建本地镜像 %s，平台 %s\n' "$IMAGE" "$PLATFORM"
docker build \
  --platform "$PLATFORM" \
  --build-arg 'GOPROXY=https://proxy.golang.org,direct' \
  --build-arg 'GOSUMDB=sum.golang.org' \
  --build-arg "VERSION=0.2.22-reasoningfix.20261118" \
  --build-arg "COMMIT=69d2fae+reasoningfix" \
  --file "$SOURCE_DIR/Dockerfile" \
  --tag "$IMAGE" \
  "$SOURCE_DIR"

docker image inspect "$IMAGE" --format '镜像 ID: {{.Id}}；平台: {{.Os}}/{{.Architecture}}'
printf '%s\n' '构建完成。请按 README 在测试实例验收；本脚本未部署或重启任何服务。'
