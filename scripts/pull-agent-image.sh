#!/usr/bin/env bash
# scripts/pull-agent-image.sh
#
# Pulls the agent runtime image via the local IPFS registry facade using digest pinning.
#
# Usage:
#   pull-agent-image.sh 127.0.0.1:5050/ipfs/<CID>@sha256:<digest>
#   pull-agent-image.sh result/publication.json
#   pull-agent-image.sh <CID> [digest]

set -euo pipefail

REGISTRY_HOST="${IPFS_REGISTRY_HOST:-127.0.0.1:5050}"
TARGET="${1:-}"

if [ -z "$TARGET" ]; then
  if [ -f "result/publication.json" ]; then
    TARGET="result/publication.json"
  else
    echo "usage: pull-agent-image.sh <reference | publication.json | CID> [digest]" >&2
    exit 1
  fi
fi

if [ -f "$TARGET" ]; then
  # Read reference from JSON file
  REFERENCE=$(jq -r '.reference // empty' "$TARGET")
  if [ -z "$REFERENCE" ]; then
    CID=$(jq -r '.cid' "$TARGET")
    DIGEST=$(jq -r '.digest' "$TARGET")
    REFERENCE="$REGISTRY_HOST/ipfs/$CID@$DIGEST"
  fi
elif [[ "$TARGET" == *"@"* ]]; then
  # Full reference provided
  REFERENCE="$TARGET"
elif [ -n "${2:-}" ]; then
  # CID + digest provided positionally
  REFERENCE="$REGISTRY_HOST/ipfs/$TARGET@$2"
else
  # Bare CID provided without digest
  REFERENCE="$REGISTRY_HOST/ipfs/$TARGET"
fi

echo "pull-agent-image: pulling $REFERENCE via IPFS registry facade..." >&2
nerdctl pull "$REFERENCE"
echo "pull-agent-image: successfully pulled $REFERENCE" >&2
