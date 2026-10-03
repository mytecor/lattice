#!/usr/bin/env bash
# scripts/publish-agent-image.sh
#
# Builds and publishes the immutable OCI agent runtime image to IPFS using nerdctl.
#
# Distribution flow:
#   nix build .#agent-image
#         ↓
#   nerdctl load --input <image-tar> (→ containerd store)
#         ↓
#   nerdctl push ipfs://<tag> (→ Kubo daemon)
#         ↓
#   IPFS CID
#         ↓
#   Resolve CID → OCI manifest digest from containerd
#         ↓
#   ImagePublication { cid, digest, reference }
#
# The final reference for r1s execution is:
#   127.0.0.1:5050/ipfs/<CID>@sha256:<digest>

set -euo pipefail

IMAGE_TAR="${1:-}"
REGISTRY_HOST="${IPFS_REGISTRY_HOST:-127.0.0.1:5050}"
IPFS_API_ADDRESS="${IPFS_API_ADDRESS:-/ip4/127.0.0.1/tcp/5001}"
IMAGE_TAG="${IMAGE_TAG:-lattice-agent-runtime:latest}"

if [ -z "$IMAGE_TAR" ]; then
  if [ -f "result" ]; then
    IMAGE_TAR="result"
  elif [ -f "result/agent-image.tar.gz" ]; then
    IMAGE_TAR="result/agent-image.tar.gz"
  else
    echo "publish-agent-image: building agent-image with nix build..." >&2
    nix build .#agent-image --out-link result-agent-image
    IMAGE_TAR="result-agent-image"
  fi
fi

if [ ! -f "$IMAGE_TAR" ]; then
  echo "publish-agent-image: image tarball not found at $IMAGE_TAR" >&2
  exit 1
fi

echo "publish-agent-image: loading $IMAGE_TAR into containerd..." >&2
LOAD_OUT=$(nerdctl load -i "$IMAGE_TAR")
echo "$LOAD_OUT" >&2

# nerdctl may report an internal `import@sha256:...` entry before the actual
# tagged image. That entry is not pushable by name, so select the last real
# image reference and fall back to the package's declared tag.
LOADED_REF=$(printf '%s\n' "$LOAD_OUT" \
  | sed -n 's/^Loaded image: //p' \
  | grep -v '^import@sha256:' \
  | tail -n 1 || true)
REF="${LOADED_REF:-$IMAGE_TAG}"

echo "publish-agent-image: pushing ipfs://$REF ..." >&2
PUSH_OUT=$(nerdctl push --ipfs-address "$IPFS_API_ADDRESS" "ipfs://$REF")
echo "$PUSH_OUT" >&2

# Extract CID from the last non-empty line of nerdctl push output
CID=$(printf '%s\n' "$PUSH_OUT" | grep -v '^$' | tail -n 1 | tr -d '[:space:]')
if [ -z "$CID" ]; then
  echo "publish-agent-image: failed to extract IPFS CID from push output" >&2
  exit 1
fi

echo "publish-agent-image: published to IPFS with CID: $CID" >&2

# Resolve OCI manifest digest from containerd / registry facade
FACADE_REF="$REGISTRY_HOST/ipfs/$CID"
echo "publish-agent-image: pulling $FACADE_REF to resolve pinned OCI digest..." >&2
nerdctl pull --quiet "$FACADE_REF" >&2

# Select the digest belonging specifically to the facade reference. An image
# can have multiple RepoDigests; taking the first sha256 value could pin an
# unrelated alias of the same image.
DIGEST=$(nerdctl image inspect --format '{{json .RepoDigests}}' "$FACADE_REF" 2>/dev/null \
  | jq -r --arg ref "$FACADE_REF" \
    '[.[] | select(startswith($ref + "@"))][0] // "" | split("@")[-1]')

if [[ ! "$DIGEST" =~ ^sha256:[a-f0-9]{64}$ ]]; then
  echo "publish-agent-image: could not resolve OCI manifest digest for $FACADE_REF" >&2
  exit 1
fi

PINNED_REF="$REGISTRY_HOST/ipfs/$CID@$DIGEST"

mkdir -p result
PUBLICATION_JSON=$(jq -n \
  --arg cid "$CID" \
  --arg digest "$DIGEST" \
  --arg reference "$PINNED_REF" \
  '{cid: $cid, digest: $digest, reference: $reference}')

echo "$PUBLICATION_JSON" > result/publication.json
echo "publish-agent-image: publication record written to result/publication.json" >&2
echo "$PUBLICATION_JSON"
