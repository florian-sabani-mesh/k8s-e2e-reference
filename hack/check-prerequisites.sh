#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
for tool in docker kind kubectl terraform go node npm make curl; do need "$tool"; done
docker info >/dev/null
node -e 'if (Number(process.versions.node.split(".")[0]) < 22) throw Error("Node.js 22+ required")'
