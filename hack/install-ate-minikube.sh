#!/usr/bin/env bash

# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit -o nounset -o pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "${ROOT}"

usage() {
  echo "Usage: KO_DOCKER_REPO=registry.example.com/substrate $0 [install-ate.sh options]"
  echo
  echo "Installs Agent Substrate into an already-running Minikube profile."
  echo "The registry must be reachable by Minikube nodes and allow them to pull images."
  echo
  echo "Environment:"
  echo "  MINIKUBE_PROFILE  Minikube profile (default: minikube-ate)."
  echo "  KUBECTL_CONTEXT   kubeconfig context (default: the profile name)."
  echo "  KO_DOCKER_REPO    Writable image repository, also pullable by cluster nodes (required)."
  echo
  echo "Example:"
  echo "  KO_DOCKER_REPO=ghcr.io/OWNER/substrate-dev $0 --deploy-ate-system --deploy-demo-counter"
}

for arg in "$@"; do
  case "${arg}" in
    -h|--help)
      usage
      echo
      sed 's/\r$//' hack/install-ate.sh | bash -s -- --help
      exit 0
      ;;
  esac
done

if [[ -z "${KO_DOCKER_REPO:-}" ]]; then
  echo "error: set KO_DOCKER_REPO to a registry writable from this shell and pullable by Minikube nodes" >&2
  usage >&2
  exit 1
fi

case "${KO_DOCKER_REPO}" in
  http://*|https://*)
    echo "error: KO_DOCKER_REPO must be a registry/repository path without a URL scheme" >&2
    exit 1
    ;;
  localhost|localhost:*|localhost/*|127.*|0.0.0.0*|ko.local|ko.local/*|\[::1\]*)
    echo "error: KO_DOCKER_REPO cannot point at this shell's loopback; Minikube nodes need to pull the images" >&2
    exit 1
    ;;
esac

for command_name in minikube kubectl go; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "error: ${command_name} is required in this Bash environment" >&2
    exit 1
  fi
done

MINIKUBE_PROFILE="${MINIKUBE_PROFILE:-minikube-ate}"
export KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-${MINIKUBE_PROFILE}}"

if ! minikube status --profile="${MINIKUBE_PROFILE}" >/dev/null; then
  echo "error: Minikube profile '${MINIKUBE_PROFILE}' is not running" >&2
  echo "       Start it first, for example: minikube start --profile=${MINIKUBE_PROFILE} --driver=podman" >&2
  exit 1
fi

if ! kubectl --context="${KUBECTL_CONTEXT}" get nodes >/dev/null; then
  echo "error: cannot reach Kubernetes context '${KUBECTL_CONTEXT}'" >&2
  exit 1
fi

# Reuse the local manifests (including in-cluster RustFS) while avoiding the
# Kind wrapper's localhost registry. The selected remote registry is used as-is.
export NO_DEV_ENV=true
export ATE_INSTALL_KIND=true
export ATE_CONTAINER_BUILDER="${ATE_CONTAINER_BUILDER:-podman}"
export KO_DEFAULTPLATFORMS="linux/$(go env GOARCH)"
export BUCKET_NAME="ate-snapshots"
unset GCE_REGION CLUSTER_LOCATION NETWORK SUBNETWORK MEMORYSTORE_INSTANCE PROJECT_ID

sed 's/\r$//' hack/install-ate.sh | bash -s -- "$@"