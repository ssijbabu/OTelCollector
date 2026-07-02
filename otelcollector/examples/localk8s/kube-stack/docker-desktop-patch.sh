#!/usr/bin/env bash
# Patch the daemon DaemonSet for Docker Desktop compatibility.
#
# Docker Desktop does not support HostToContainer (rslave) mount propagation,
# so the hostmetrics receiver's root_path mount causes a CrashLoopBackOff.
# Remove root_path and the process/processes scrapers that require it.
#
# Apply once after the daemon pods are created (even if they are crash-looping).

set -euo pipefail

NS=opentelemetry-operator-system
COL=opentelemetry-kube-stack-daemon

echo "Patching $COL in $NS ..."

kubectl patch opentelemetrycollector "$COL" \
  -n "$NS" \
  --type=json \
  --patch='[
    {"op":"remove","path":"/spec/config/receivers/hostmetrics/root_path"},
    {"op":"remove","path":"/spec/config/receivers/hostmetrics/scrapers/process"},
    {"op":"remove","path":"/spec/config/receivers/hostmetrics/scrapers/processes"}
  ]'

echo "Waiting for daemon rollout..."
kubectl rollout status daemonset \
  -l app.kubernetes.io/name=opentelemetry-kube-stack-daemon-collector \
  -n "$NS" --timeout=120s

echo "Done."
