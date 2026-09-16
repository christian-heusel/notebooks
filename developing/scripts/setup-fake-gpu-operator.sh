#!/usr/bin/env bash

# Setup script for the run-ai/fake-gpu-operator (FGO).
# This installs FGO into the Kind cluster so GPU workspaces (e.g. the sample
# `big_gpu` podConfig) can schedule and run without real GPU hardware: FGO's
# device-plugin advertises `nvidia.com/gpu` capacity on the labeled worker and
# injects a fake `nvidia-smi` into GPU pods.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEVELOPING_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Chart pinned during the validation spike (matches its published topology
# schema: topology.nodePools.<pool>.{gpuProduct,gpuCount,gpuMemory}).
FGO_CHART="oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator"
FGO_VERSION="0.2.0"
NAMESPACE="gpu-operator"

# nvml-mock ("Mokka") — the optional ENABLE_FAKE_GPU_NVML layer. It simulates
# GPUs at the driver level (a real libnvidia-ml.so), so `nvidia-smi`/DCGM return
# realistic profile-accurate data inside GPU pods, not just FGO's limited fake.
NVML_CHART="oci://ghcr.io/nvidia/k8s-test-infra/chart/nvml-mock"
NVML_VERSION="0.3.0"

# ENABLE_FAKE_GPU_NVML implies the base FGO path (Mokka layers on top of it).
ENABLE_FAKE_GPU_NVML="${ENABLE_FAKE_GPU_NVML:-false}"
if [[ "${ENABLE_FAKE_GPU_NVML}" == "true" ]]; then
  ENABLE_FAKE_GPU="true"
fi

# Do nothing if neither ENABLE_FAKE_GPU nor ENABLE_FAKE_GPU_NVML is set to true
if [[ "${ENABLE_FAKE_GPU:-false}" != "true" ]]; then
  echo ""
  echo ""
  echo "INFO: fake-gpu-operator setup is disabled. Set ENABLE_FAKE_GPU=true to enable"
  echo "      (or ENABLE_FAKE_GPU_NVML=true for realistic in-pod nvidia-smi via nvml-mock)."
  echo ""
  echo ""
  exit 0
fi

# helm is only required for the GPU path, so we check for it here rather than
# as a global prerequisite.
if ! command -v helm >/dev/null 2>&1; then
  echo "ERROR: helm is required for ENABLE_FAKE_GPU=true. Please install helm first:"
  echo "  https://helm.sh/docs/intro/install/"
  exit 1
fi

# ================================
# Sanity checks: refuse to run alongside a real NVIDIA GPU stack
# ================================
# FGO simulates GPUs at the API level. Co-installing it with the real NVIDIA
# GPU Operator or a real device plugin produces conflicting nvidia.com/gpu
# advertisements, so bail out (before mutating anything) if we detect one.

# 1. The real NVIDIA GPU Operator / device plugin ships these DaemonSets; FGO
#    never creates them (its own device-plugin DaemonSet is named "device-plugin").
if kubectl get daemonset --all-namespaces \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null \
  | grep -Eqx 'nvidia-driver-daemonset|nvidia-device-plugin-daemonset'; then
  echo "ERROR: a real NVIDIA GPU Operator / device plugin appears to be installed"
  echo "       (found an 'nvidia-driver-daemonset' or 'nvidia-device-plugin-daemonset')."
  echo "       Refusing to install the fake-gpu-operator alongside real GPU tooling."
  exit 1
fi

# 2. No node should already advertise nvidia.com/gpu capacity, unless it came
#    from a previous run of this script (i.e. our own FGO release exists). GPU
#    capacity on a node this script hasn't touched means real hardware or a real
#    device plugin is present.
if ! helm status fgo -n "${NAMESPACE}" >/dev/null 2>&1; then
  # `|| true` keeps `set -e`/`pipefail` from aborting when grep finds no GPU node.
  GPU_NODES="$(kubectl get nodes \
    -o jsonpath='{range .items[*]}{.metadata.name}={.status.capacity.nvidia\.com/gpu}{"\n"}{end}' 2>/dev/null \
    | grep -E '=[1-9][0-9]*$' | cut -d= -f1 | tr '\n' ' ' || true)"
  if [[ -n "${GPU_NODES// }" ]]; then
    echo "ERROR: node(s) already advertise nvidia.com/gpu capacity: ${GPU_NODES}"
    echo "       This script has not installed the fake-gpu-operator here, so real"
    echo "       GPUs or a real device plugin may be present. Refusing to continue."
    exit 1
  fi
fi

# ================================
# Namespace + Pod Security Admission
# ================================
# FGO's device-plugin runs privileged pods, so the namespace must permit them.
echo "Ensuring namespace '${NAMESPACE}' exists with privileged Pod Security..."
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace "${NAMESPACE}" \
  pod-security.kubernetes.io/enforce=privileged --overwrite

# ================================
# Label the worker node(s)
# ================================
# FGO assigns nodes to the "integration" pool (see fgo.values.yaml) by this
# label. Applied at runtime; no node recreate is needed.
echo "Labeling worker node(s) for the 'integration' GPU node pool..."
kubectl label node -l '!node-role.kubernetes.io/control-plane' \
  run.ai/simulated-gpu-node-pool=integration --overwrite

# Opt kind's local-path storage helper pod out of nvml-mock's NRI injection.
#
# The NRI plugin injects the mock NVML driver overlay into EVERY container on
# the GPU worker (opt-out, not opt-in). local-path provisions each PVC via a
# short-lived helper pod that lands on the worker; its image's libc is older
# than the injected .so files require (GLIBC_2.34), so the helper cannot start,
# PVC provisioning times out, and workspace pods hang Pending on volume binding.
# Adding Mokka's opt-out annotation to the helper template skips injection for
# this storage-plumbing pod (which has no use for a GPU). Idempotent; a merge
# patch on data.helperPod.yaml leaves the ConfigMap's other keys intact.
optout_local_path_helper() {
  local ns="local-path-storage" cm="local-path-config" key="helperPod.yaml"
  local current
  # The data key contains a dot, so it must be escaped in the jsonpath
  # (an unescaped `.data.helperPod.yaml` reads as a nested path and yields "").
  current="$(kubectl get cm "${cm}" -n "${ns}" -o 'jsonpath={.data.helperPod\.yaml}' 2>/dev/null || true)"
  if [[ -z "${current}" ]]; then
    echo "WARNING: ConfigMap ${ns}/${cm} not found; skipping local-path helper opt-out."
    return 0
  fi
  if grep -q 'nvml-mock.nvidia.com/inject' <<<"${current}"; then
    echo "local-path helper pod already opted out of nvml-mock injection."
    return 0
  fi
  echo "Opting the local-path helper pod out of nvml-mock NRI injection..."
  local patched patch_file
  # Insert an annotations block right after the pod's `metadata:` line.
  patched="$(awk '
    /^metadata:/ && !done { print; print "  annotations:"; print "    nvml-mock.nvidia.com/inject: \"false\""; done=1; next }
    { print }
  ' <<<"${current}")"
  patch_file="$(mktemp)"
  {
    echo "data:"
    echo "  ${key}: |"
    sed 's/^/    /' <<<"${patched}"
  } >"${patch_file}"
  kubectl patch cm "${cm}" -n "${ns}" --type merge --patch-file "${patch_file}"
  rm -f "${patch_file}"
}

# ================================
# (Optional) Install nvml-mock ("Mokka") for realistic in-pod nvidia-smi
# ================================
# On the ENABLE_FAKE_GPU_NVML path, Mokka is installed FIRST: it emits the
# `gpu-profile-<profile>` ConfigMaps that FGO's loader reads, and FGO is then
# installed with builtinProfiles disabled + the pool's `backend: mock` (see
# fgo.nvml.values.yaml) so it defers to those ConfigMaps. Order matters — FGO
# with builtinProfiles=false needs the ConfigMaps to already exist.
FGO_VALUES="${DEVELOPING_DIR}/manifests/fake-gpu-operator/fgo.values.yaml"
if [[ "${ENABLE_FAKE_GPU_NVML}" == "true" ]]; then
  FGO_VALUES="${DEVELOPING_DIR}/manifests/fake-gpu-operator/fgo.nvml.values.yaml"

  # Mokka's in-pod injection rides containerd NRI, which the standard dev
  # cluster only gains on recreate (see developing/kind-1-35.yaml). Warn loudly
  # rather than fail: the ConfigMap/scheduling path still works without NRI, and
  # we can't reliably detect the containerd config from here.
  echo ""
  echo "NOTE: ENABLE_FAKE_GPU_NVML=true needs containerd NRI enabled on the node"
  echo "      for real libnvidia-ml.so injection into GPU pods. If GPU workspace"
  echo "      pods do not get a realistic nvidia-smi, recreate the cluster so it"
  echo "      picks up developing/kind-1-35.yaml: 'make tilt-down && make tilt-up'."
  echo ""

  # Do this BEFORE Mokka's NRI plugin starts, so no helper pod is ever injected.
  optout_local_path_helper

  echo "Installing/upgrading nvml-mock (${NVML_VERSION})..."
  helm upgrade -i nvml-mock "${NVML_CHART}" \
    --namespace "${NAMESPACE}" \
    --version "${NVML_VERSION}" \
    -f "${DEVELOPING_DIR}/manifests/fake-gpu-operator/nvml-mock.values.yaml" \
    --wait
fi

# ================================
# Install the fake-gpu-operator
# ================================
echo "Installing/upgrading fake-gpu-operator (${FGO_VERSION})..."
helm upgrade -i fgo "${FGO_CHART}" \
  --namespace "${NAMESPACE}" \
  --version "${FGO_VERSION}" \
  -f "${FGO_VALUES}" \
  --wait

# ================================
# Wait for GPU capacity to appear
# ================================
echo "Waiting for the worker to advertise nvidia.com/gpu capacity..."
until kubectl get node -l run.ai/simulated-gpu-node-pool=integration \
  -o jsonpath='{.items[0].status.capacity.nvidia\.com/gpu}' 2>/dev/null | grep -q '[1-9]'; do
  echo "... (no GPU capacity yet)"
  sleep 3
done

echo "fake-gpu-operator setup complete"
kubectl get nodes -o custom-columns=NAME:.metadata.name,GPU:'.status.capacity.nvidia\.com/gpu'

if [[ "${ENABLE_FAKE_GPU_NVML}" == "true" ]]; then
  echo ""
  echo "nvml-mock (Mokka) is installed. Verify a realistic in-pod nvidia-smi with:"
  echo "  kubectl exec -n ${NAMESPACE} ds/nvml-mock -- nvidia-smi"
  echo "and, once a GPU workspace pod is running, exec nvidia-smi inside it."
fi
