#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
tf init -input=false -backend-config="path=$ROOT/.run/terraform.tfstate"
tf validate
tf apply -input=false -auto-approve
