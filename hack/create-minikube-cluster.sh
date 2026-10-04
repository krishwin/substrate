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

MINIKUBE_PROFILE="${MINIKUBE_PROFILE:-minikube-ate}"
MINIKUBE_DRIVER="${MINIKUBE_DRIVER:-podman}"
MINIKUBE_CONTAINER_RUNTIME="${MINIKUBE_CONTAINER_RUNTIME:-containerd}"
MINIKUBE_CPUS="${MINIKUBE_CPUS:-4}"
MINIKUBE_MEMORY="${MINIKUBE_MEMORY:-8192}"
MINIKUBE_DISK_SIZE="${MINIKUBE_DISK_SIZE:-40g}"
KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-${MINIKUBE_PROFILE}}"

usage() {
  echo "Usage: $0 [additional minikube start options]"
  echo
  echo "Creates a Minikube profile with the Kubernetes APIs required by Agent Substrate."
  echo "It refuses to reuse an existing profile and never deletes a cluster."
  echo
  echo "Environment:"
  echo "  MINIKUBE_PROFILE          Profile name (default: minikube-ate)."
  echo "  MINIKUBE_DRIVER           Minikube driver (default: podman)."
  echo "  MINIKUBE_CONTAINER_RUNTIME Kubernetes node runtime (default: containerd)."
  echo "  MINIKUBE_CPUS             CPU count (default: 4)."
  echo "  MINIKUBE_MEMORY           Memory in MiB (default: 8192)."
  echo "  MINIKUBE_DISK_SIZE        Disk size (default: 40g)."
  echo "  KUBECTL_CONTEXT           kubeconfig context (default: profile name)."
}

for arg in "$@"; do
  case "${arg}" in
    -h|--help)
      usage
      exit 0
      ;;
  esac
done

if [[ ! "${MINIKUBE_PROFILE}" =~ ^[a-zA-Z0-9][a-zA-Z0-9-]*$ ]]; then
  echo "error: MINIKUBE_PROFILE may contain only letters, digits, and hyphens" >&2
  exit 1
fi

for command_name in minikube kubectl; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "error: ${command_name} is required in this Bash environment" >&2
    exit 1
  fi
done

if [[ "${MINIKUBE_DRIVER}" == "podman" ]] && ! command -v podman >/dev/null 2>&1; then
  echo "error: podman is required when MINIKUBE_DRIVER=podman" >&2
  exit 1
fi

if minikube profile list -o json | grep -Eq "\"Name\"[[:space:]]*:[[:space:]]*\"${MINIKUBE_PROFILE}\""; then
  echo "error: Minikube profile '${MINIKUBE_PROFILE}' already exists; choose a new MINIKUBE_PROFILE" >&2
  exit 1
fi

echo "Creating Minikube profile '${MINIKUBE_PROFILE}' with driver '${MINIKUBE_DRIVER}'..."
minikube start \
  --profile="${MINIKUBE_PROFILE}" \
  --driver="${MINIKUBE_DRIVER}" \
  --container-runtime="${MINIKUBE_CONTAINER_RUNTIME}" \
  --cpus="${MINIKUBE_CPUS}" \
  --memory="${MINIKUBE_MEMORY}" \
  --disk-size="${MINIKUBE_DISK_SIZE}" \
  --feature-gates=ClusterTrustBundle=true,ClusterTrustBundleProjection=true,PodCertificateRequest=true \
  --extra-config=apiserver.runtime-config=certificates.k8s.io/v1beta1=true \
  "$@"

kubectl --context="${KUBECTL_CONTEXT}" wait \
  --for=condition=Ready nodes --all --timeout=10m

resources="$(kubectl --context="${KUBECTL_CONTEXT}" api-resources \
  --api-group=certificates.k8s.io -o name)"
for required_resource in \
  clustertrustbundles.certificates.k8s.io \
  podcertificaterequests.certificates.k8s.io; do
  if ! grep -Fxq "${required_resource}" <<<"${resources}"; then
    echo "error: API resource '${required_resource}' is unavailable in profile '${MINIKUBE_PROFILE}'" >&2
    echo "       Check the Minikube Kubernetes version and feature-gate support." >&2
    exit 1
  fi
done

echo "Minikube profile '${MINIKUBE_PROFILE}' is ready for Agent Substrate."
echo "Kube context: ${KUBECTL_CONTEXT}"
echo "Set KO_DOCKER_REPO to a registry writable from this shell and pullable by the cluster before installing."