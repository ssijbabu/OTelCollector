#!/usr/bin/env bash
# cleanup.sh — Delete all Azure resources created by setup.sh
#
# Deletes:
#   - Resource group (and everything inside it: AKS, ACR, Event Hubs)
#   - App Registration + Service Principal (these live in AAD, not the resource group)
#   - Federated credentials are deleted automatically with the app registration
#
# Usage:
#   export AZURE_SUBSCRIPTION_ID=<your-subscription-id>
#   ./cleanup.sh
#
# Set DRY_RUN=1 to print what would be deleted without actually deleting.

set -euo pipefail

# ── Configuration (must match setup.sh) ───────────────────────────────────────

SUBSCRIPTION_ID="${AZURE_SUBSCRIPTION_ID:?Set AZURE_SUBSCRIPTION_ID before running}"
RESOURCE_GROUP="${RESOURCE_GROUP:-otel-test-rg}"
APP_NAME="${APP_NAME:-otel-test-spn}"
DRY_RUN="${DRY_RUN:-0}"

# ── Helpers ───────────────────────────────────────────────────────────────────

info()    { echo ""; echo "==> $*"; }
success() { echo "    ✓ $*"; }
warn()    { echo "    ! $*"; }

run() {
    if [ "$DRY_RUN" = "1" ]; then
        echo "    [dry-run] $*"
    else
        "$@"
    fi
}

# ── Confirm ───────────────────────────────────────────────────────────────────

echo ""
echo "════════════════════════════════════════════════════════════════"
echo " OTel Collector test environment cleanup"
echo "════════════════════════════════════════════════════════════════"
echo ""
echo " Resource group : $RESOURCE_GROUP"
echo " App name       : $APP_NAME"
echo " Subscription   : $SUBSCRIPTION_ID"
if [ "$DRY_RUN" = "1" ]; then
    echo " Mode           : DRY RUN (nothing will be deleted)"
fi
echo ""

if [ "$DRY_RUN" != "1" ]; then
    read -r -p " Type 'yes' to confirm deletion: " CONFIRM
    if [ "$CONFIRM" != "yes" ]; then
        echo " Aborted."
        exit 0
    fi
fi

# ── 1. Login check ────────────────────────────────────────────────────────────

info "Checking Azure CLI login"
az account set --subscription "$SUBSCRIPTION_ID"
success "Subscription set"

# ── 2. App Registration ───────────────────────────────────────────────────────

info "App Registration: $APP_NAME"
APP_ID=$(az ad app list --display-name "$APP_NAME" --query "[0].appId" --output tsv 2>/dev/null || true)

if [ -z "$APP_ID" ] || [ "$APP_ID" = "None" ]; then
    warn "App registration not found — skipping"
else
    # Delete the service principal first (role assignments are removed automatically)
    SP_OID=$(az ad sp show --id "$APP_ID" --query "id" --output tsv 2>/dev/null || true)
    if [ -n "$SP_OID" ] && [ "$SP_OID" != "None" ]; then
        run az ad sp delete --id "$SP_OID"
        success "Deleted service principal (Object ID: $SP_OID)"
    fi

    # Delete the app registration (also removes all federated credentials)
    run az ad app delete --id "$APP_ID"
    success "Deleted app registration (Client ID: $APP_ID)"
fi

# ── 3. Resource group ─────────────────────────────────────────────────────────

info "Resource group: $RESOURCE_GROUP"
if ! az group show --name "$RESOURCE_GROUP" &>/dev/null; then
    warn "Resource group not found — skipping"
else
    echo "    Deleting resource group and all contents"
    echo "    (AKS, ACR, Event Hubs namespace + hubs, etc.)"
    echo "    This may take several minutes..."
    run az group delete \
        --name "$RESOURCE_GROUP" \
        --yes \
        --no-wait
    if [ "$DRY_RUN" != "1" ]; then
        success "Deletion initiated (running async — check portal or run: az group show -n $RESOURCE_GROUP)"
    fi
fi

# ── Done ──────────────────────────────────────────────────────────────────────

echo ""
echo "════════════════════════════════════════════════════════════════"
if [ "$DRY_RUN" = "1" ]; then
    echo " Dry run complete — nothing was deleted"
else
    echo " Cleanup initiated"
    echo ""
    echo " The resource group deletion runs asynchronously."
    echo " To check status:"
    echo "   az group show --name $RESOURCE_GROUP --query 'properties.provisioningState'"
    echo ""
    echo " Once complete, verify nothing remains:"
    echo "   az group list --query \"[?name=='$RESOURCE_GROUP']\""
    echo "   az ad app list --display-name '$APP_NAME'"
fi
echo "════════════════════════════════════════════════════════════════"
