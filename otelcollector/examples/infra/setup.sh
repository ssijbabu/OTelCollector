#!/usr/bin/env bash
# setup.sh — Provision Azure resources for OTel Collector testing
#
# Creates:
#   - Resource group
#   - AKS cluster (OIDC issuer + workload identity enabled)
#   - Azure Container Registry, attached to AKS
#   - Event Hubs namespace (Standard SKU — required for Kafka protocol)
#   - 3 Event Hubs: otel-logs, otel-metrics, otel-traces
#   - App Registration (SPN) with client secret
#   - Role assignments: Data Sender + Data Receiver on the Event Hubs namespace
#   - Federated credentials on the SPN for the collector and gateway service accounts
#
# Usage:
#   export AZURE_SUBSCRIPTION_ID=<your-subscription-id>
#   ./setup.sh
#
# All resource names are derived from the variables below.
# Re-running the script is safe — existing resources are skipped or updated.

set -euo pipefail

# ── Configuration ─────────────────────────────────────────────────────────────

SUBSCRIPTION_ID="${AZURE_SUBSCRIPTION_ID:?Set AZURE_SUBSCRIPTION_ID before running}"
RESOURCE_GROUP="${RESOURCE_GROUP:-otel-test-rg}"
LOCATION="${LOCATION:-eastus}"
CLUSTER_NAME="${CLUSTER_NAME:-otel-test-aks}"
ACR_NAME="${ACR_NAME:-oteltestacr}"           # must be globally unique, alphanumeric
EH_NAMESPACE="${EH_NAMESPACE:-otel-test-eh}"  # must be globally unique
APP_NAME="${APP_NAME:-otel-test-spn}"
ENVIRONMENT="${ENVIRONMENT:-dev}"

# Kubernetes namespaces and service account names (must match the manifest files)
K8S_COLLECTOR_NS="otel-collector"
K8S_COLLECTOR_SA="otel-collector"
K8S_GATEWAY_NS="otel-gateway"
K8S_GATEWAY_SA="otel-gateway"

# Event Hub names (must match env-configmap.yaml)
EH_LOGS="otel-logs"
EH_METRICS="otel-metrics"
EH_TRACES="otel-traces"

# ── Helpers ───────────────────────────────────────────────────────────────────

info()    { echo ""; echo "==> $*"; }
success() { echo "    ✓ $*"; }

# ── 1. Login check ────────────────────────────────────────────────────────────

info "Checking Azure CLI login"
az account set --subscription "$SUBSCRIPTION_ID"
success "Subscription: $SUBSCRIPTION_ID"

# ── 2. Resource group ─────────────────────────────────────────────────────────

info "Resource group: $RESOURCE_GROUP"
az group create \
    --name "$RESOURCE_GROUP" \
    --location "$LOCATION" \
    --output none
success "Created/confirmed $RESOURCE_GROUP in $LOCATION"

# ── 3. AKS cluster ────────────────────────────────────────────────────────────

info "AKS cluster: $CLUSTER_NAME"
if az aks show --resource-group "$RESOURCE_GROUP" --name "$CLUSTER_NAME" &>/dev/null; then
    success "Already exists — skipping creation"
else
    az aks create \
        --resource-group "$RESOURCE_GROUP" \
        --name "$CLUSTER_NAME" \
        --location "$LOCATION" \
        --node-count 2 \
        --node-vm-size Standard_D2s_v3 \
        --enable-oidc-issuer \
        --enable-workload-identity \
        --generate-ssh-keys \
        --output none
    success "Created $CLUSTER_NAME"
fi

OIDC_ISSUER=$(az aks show \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CLUSTER_NAME" \
    --query "oidcIssuerProfile.issuerUrl" \
    --output tsv)
success "OIDC issuer: $OIDC_ISSUER"

# ── 4. Azure Container Registry ───────────────────────────────────────────────

info "Container Registry: $ACR_NAME"
if az acr show --name "$ACR_NAME" --resource-group "$RESOURCE_GROUP" &>/dev/null; then
    success "Already exists — skipping creation"
else
    az acr create \
        --resource-group "$RESOURCE_GROUP" \
        --name "$ACR_NAME" \
        --sku Basic \
        --output none
    success "Created $ACR_NAME"
fi

info "Attaching ACR to AKS"
az aks update \
    --resource-group "$RESOURCE_GROUP" \
    --name "$CLUSTER_NAME" \
    --attach-acr "$ACR_NAME" \
    --output none
success "AKS can pull from $ACR_NAME"

ACR_LOGIN_SERVER=$(az acr show \
    --name "$ACR_NAME" \
    --resource-group "$RESOURCE_GROUP" \
    --query "loginServer" \
    --output tsv)
success "ACR login server: $ACR_LOGIN_SERVER"

# ── 5. Event Hubs namespace ───────────────────────────────────────────────────

info "Event Hubs namespace: $EH_NAMESPACE"
if az eventhubs namespace show --resource-group "$RESOURCE_GROUP" --name "$EH_NAMESPACE" &>/dev/null; then
    success "Already exists — skipping creation"
else
    az eventhubs namespace create \
        --resource-group "$RESOURCE_GROUP" \
        --name "$EH_NAMESPACE" \
        --location "$LOCATION" \
        --sku Standard \
        --output none
    success "Created $EH_NAMESPACE (Standard SKU — Kafka enabled)"
fi

EH_FQDN="${EH_NAMESPACE}.servicebus.windows.net"

# ── 6. Event Hubs ─────────────────────────────────────────────────────────────

info "Event Hubs: $EH_LOGS, $EH_METRICS, $EH_TRACES"
for HUB in "$EH_LOGS" "$EH_METRICS" "$EH_TRACES"; do
    if az eventhubs eventhub show \
            --resource-group "$RESOURCE_GROUP" \
            --namespace-name "$EH_NAMESPACE" \
            --name "$HUB" &>/dev/null; then
        success "$HUB — already exists"
    else
        az eventhubs eventhub create \
            --resource-group "$RESOURCE_GROUP" \
            --namespace-name "$EH_NAMESPACE" \
            --name "$HUB" \
            --partition-count 4 \
            --retention-time-in-hours 24 \
            --output none
        success "$HUB — created"
    fi
done

EH_NAMESPACE_ID=$(az eventhubs namespace show \
    --resource-group "$RESOURCE_GROUP" \
    --name "$EH_NAMESPACE" \
    --query "id" \
    --output tsv)

# ── 7. App Registration (SPN) ─────────────────────────────────────────────────

info "App Registration: $APP_NAME"
APP_ID=$(az ad app list --display-name "$APP_NAME" --query "[0].appId" --output tsv)

if [ -z "$APP_ID" ] || [ "$APP_ID" = "None" ]; then
    APP_ID=$(az ad app create \
        --display-name "$APP_NAME" \
        --query "appId" \
        --output tsv)
    success "Created app registration — Client ID: $APP_ID"
else
    success "Already exists — Client ID: $APP_ID"
fi

# Ensure a service principal exists for this app
SP_OID=$(az ad sp show --id "$APP_ID" --query "id" --output tsv 2>/dev/null || true)
if [ -z "$SP_OID" ] || [ "$SP_OID" = "None" ]; then
    SP_OID=$(az ad sp create --id "$APP_ID" --query "id" --output tsv)
    success "Created service principal — Object ID: $SP_OID"
else
    success "Service principal exists — Object ID: $SP_OID"
fi

# ── 8. Role assignments ───────────────────────────────────────────────────────

info "Role assignments on Event Hubs namespace"

for ROLE in "Azure Event Hubs Data Sender" "Azure Event Hubs Data Receiver"; do
    EXISTING=$(az role assignment list \
        --assignee "$APP_ID" \
        --scope "$EH_NAMESPACE_ID" \
        --role "$ROLE" \
        --query "[0].id" \
        --output tsv 2>/dev/null || true)
    if [ -n "$EXISTING" ] && [ "$EXISTING" != "None" ]; then
        success "$ROLE — already assigned"
    else
        az role assignment create \
            --assignee "$APP_ID" \
            --scope "$EH_NAMESPACE_ID" \
            --role "$ROLE" \
            --output none
        success "$ROLE — assigned"
    fi
done

# ── 9. Federated credentials ──────────────────────────────────────────────────

info "Federated credentials (workload identity)"

create_federated_credential() {
    local CRED_NAME="$1"
    local K8S_NS="$2"
    local K8S_SA="$3"
    local SUBJECT="system:serviceaccount:${K8S_NS}:${K8S_SA}"

    EXISTING=$(az ad app federated-credential list \
        --id "$APP_ID" \
        --query "[?name=='${CRED_NAME}'].id" \
        --output tsv 2>/dev/null || true)

    if [ -n "$EXISTING" ] && [ "$EXISTING" != "None" ]; then
        success "$CRED_NAME — already exists"
    else
        az ad app federated-credential create \
            --id "$APP_ID" \
            --parameters "{
                \"name\": \"${CRED_NAME}\",
                \"issuer\": \"${OIDC_ISSUER}\",
                \"subject\": \"${SUBJECT}\",
                \"audiences\": [\"api://AzureADTokenExchange\"]
            }" \
            --output none
        success "$CRED_NAME — created (subject: $SUBJECT)"
    fi
}

create_federated_credential "otel-collector-fedcred" "$K8S_COLLECTOR_NS" "$K8S_COLLECTOR_SA"
create_federated_credential "otel-gateway-fedcred"   "$K8S_GATEWAY_NS"   "$K8S_GATEWAY_SA"

# ── 10. Summary ───────────────────────────────────────────────────────────────

TENANT_ID=$(az account show --query "tenantId" --output tsv)

echo ""
echo "════════════════════════════════════════════════════════════════"
echo " Setup complete"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo " Azure resources"
echo "   Resource group   : $RESOURCE_GROUP"
echo "   AKS cluster      : $CLUSTER_NAME"
echo "   ACR              : $ACR_LOGIN_SERVER"
echo "   Event Hubs FQDN  : $EH_FQDN"
echo "   App Client ID    : $APP_ID"
echo "   Tenant ID        : $TENANT_ID"
echo ""
echo " Next steps"
echo ""
echo " 1. Build and push the collector image:"
echo "    az acr login --name $ACR_NAME"
echo "    docker build -f otelcollector/Dockerfile -t ${ACR_LOGIN_SERVER}/otelcollector:latest ."
echo "    docker push ${ACR_LOGIN_SERVER}/otelcollector:latest"
echo ""
echo " 2. Get AKS credentials:"
echo "    az aks get-credentials --resource-group $RESOURCE_GROUP --name $CLUSTER_NAME"
echo ""
echo " 3. Update env-configmaps with these values:"
echo "    namespace              : $EH_FQDN"
echo "    logs-hub-name          : $EH_LOGS"
echo "    metrics-hub-name       : $EH_METRICS"
echo "    traces-hub-name        : $EH_TRACES"
echo "    k8s.cluster.name       : $CLUSTER_NAME"
echo "    cloud.region           : $LOCATION"
echo "    deployment.environment : $ENVIRONMENT"
echo ""
echo " 4. Update service account annotations with Client ID:"
echo "    azure.workload.identity/client-id: $APP_ID"
echo ""
echo " 5. Update deployment image references:"
echo "    image: ${ACR_LOGIN_SERVER}/otelcollector:latest"
echo ""
echo " 6. Apply manifests:"
echo "    kubectl apply -f examples/k8s/collector/"
echo "    kubectl apply -f examples/k8s/gateway/"
echo "════════════════════════════════════════════════════════════════"
