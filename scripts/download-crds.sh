#!/usr/bin/env bash
# download-crds.sh - Download CRDs from upstream sources
#
# Usage:
#   ./scripts/download-crds.sh [--all|capi]
#
# Environment variables:
#   CAPI_VERSION - Cluster API version (default: v1.11.2)
#
# Examples:
#   ./scripts/download-crds.sh --all         # Download all CRDs
#   ./scripts/download-crds.sh capi          # Download CAPI CRDs only
#   CAPI_VERSION=v1.10.0 ./scripts/download-crds.sh capi

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PROJECT_ROOT=$(cd "${SCRIPT_DIR}/.." && pwd)
OUTPUT_DIR="${PROJECT_ROOT}/crds"

# Version defaults (can be overridden via environment)
: "${CAPI_VERSION:=v1.11.2}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# CAPI core CRDs to download
CAPI_CRDS=(
    "cluster.x-k8s.io_clusters.yaml"
    "cluster.x-k8s.io_machines.yaml"
    "cluster.x-k8s.io_machinesets.yaml"
    "cluster.x-k8s.io_machinedeployments.yaml"
    "cluster.x-k8s.io_clusterclasses.yaml"
    "cluster.x-k8s.io_machinepools.yaml"
    "cluster.x-k8s.io_machinehealthchecks.yaml"
)

# Ensure output directory exists
ensure_output_dir() {
    mkdir -p "${OUTPUT_DIR}"
}

# Download a single file
download_file() {
    local url="$1"
    local output="$2"

    if command -v curl > /dev/null; then
        curl -fsSL "${url}" -o "${output}"
    elif command -v wget > /dev/null; then
        wget -q "${url}" -O "${output}"
    else
        printf "${RED}Error: curl or wget required${NC}\n" >&2
        exit 1
    fi
}

# Download CAPI core CRDs
download_capi() {
    local base_url="https://raw.githubusercontent.com/kubernetes-sigs/cluster-api/${CAPI_VERSION}/config/crd/bases"

    printf "${CYAN}Downloading CAPI CRDs (${CAPI_VERSION})...${NC}\n"

    for crd in "${CAPI_CRDS[@]}"; do
        local url="${base_url}/${crd}"
        local output="${OUTPUT_DIR}/${crd}"

        printf "  Downloading ${crd}..."
        if download_file "${url}" "${output}"; then
            printf " ${GREEN}OK${NC}\n"
        else
            printf " ${RED}FAILED${NC}\n"
            return 1
        fi
    done

    printf "${GREEN}CAPI CRDs downloaded successfully${NC}\n"
}

# Download all CRDs
download_all() {
    download_capi
    # Add more providers here as needed:
    # download_capa
    # download_capz
    # etc.
}

# Print usage
usage() {
    cat <<EOF
Usage: $(basename "$0") [--all|capi]

Download CRDs from upstream sources.

Options:
    --all       Download all CRDs (default)
    capi        Download CAPI core CRDs only

Environment variables:
    CAPI_VERSION    Cluster API version (default: ${CAPI_VERSION})

Examples:
    $(basename "$0") --all
    $(basename "$0") capi
    CAPI_VERSION=v1.10.0 $(basename "$0") capi
EOF
}

main() {
    local command="${1:---all}"

    ensure_output_dir

    case "${command}" in
        --all)
            download_all
            ;;
        capi)
            download_capi
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            printf "${RED}Unknown command: ${command}${NC}\n" >&2
            usage >&2
            exit 1
            ;;
    esac
}

main "$@"
