#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
if [[ -z "${SOURCE_DIR:-}" ]]; then
  if [[ -f "$SCRIPT_DIR/../../Dockerfile" ]]; then
    SOURCE_DIR="$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)"
  else
    SOURCE_DIR="$SCRIPT_DIR/sub2api"
  fi
fi
test -f "$SOURCE_DIR/backend/go.mod"
docker run --rm \
  -v "$SOURCE_DIR/backend:/work:ro" -w /work \
  -e GOPROXY=https://proxy.golang.org,direct -e GOSUMDB=sum.golang.org \
  -e GOMAXPROCS=2 \
  -v sub2api-reasoning-fix-gomod:/go/pkg/mod \
  -v sub2api-reasoning-fix-gobuild:/root/.cache/go-build \
  golang:1.27.4-alpine sh -ec '
    go test -tags=unit ./internal/pkg/apicompat -count=1
    go test -tags=unit ./internal/service -run "TestForwardAsChatCompletions|TestChatReasoning|TestNormalizeOpenAIAPIKeyStoreFalseReasoningReplay|TestOmitSyntheticChatReasoningForNativeResponses" -count=1 -timeout=240s
  '
