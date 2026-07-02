#!/usr/bin/env bash
# cleanup.sh — Delete all Azure resources created by setup.sh
#
# Deletes (in order):
#   1. Federated credentials     — on the App Registration
#   2. App Registration + SP     — live in AAD, not in the resource group
#   3. Resource group            — AKS, MC_* node group, ACR, Event Hubs namespace
#                                  + all hubs, consumer groups, role assignments
#
# NOTE: Azure auto-creates a NetworkWatcherRG in each region when you create
#       an AKS cluster. This script does NOT delete it — it is shared
#       infrastructure that may be used by other resources in your subscription.
#
# Usage:
#   export AZURE_SUBSCRIPTION_ID=<your-subscription-id>
#   ./cleanup.sh
#
# Options (env vars):
#   DRY_RUN=1   Print what would be deleted without making any changes.
#   ASYNC=1     Do not wait for the resource group deletion to complete.

set -euo pipefail

# ── Configuration (must match setup.sh) ───────────────────────────────────────

SUBSCRIPTION_ID="${AZURE_SUBSCRIPTION_ID:?Set AZURE_SUBSCRIPTION_ID before running}"
RESOURCE_GROUP="${RESOURCE_GROUP:-otel-test-rg}"
APP_NAME="${APP_NAME:-otel-test-spn}"
DRY_RUN="${DRY_RUN:-0}"
ASYNC="${ASYNC:-0}"

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

# ── 2. Federated credentials ──────────────────────────────────────────────────

info "App Registration: $APP_NAME"
APP_ID=$(az ad app list --display-name "$APP_NAME" --query "[0].appId" --output tsv 2>/dev/null || true)

if [ -z "$APP_ID" ] || [ "$APP_ID" = "None" ]; then
    warn "App registration not found — skipping AAD cleanup"
else
    success "Found app registration — Client ID: $APP_ID"

    # Delete federated credentials explicitly (informational — also removed with app reg)
    info "Federated credentials on $APP_NAME"
    CRED_IDS=$(az ad app federated-credential list \
        --id "$APP_ID" \
        --query "[].id" \
        --output tsv 2>/dev/null || true)

    if [ -z "$CRED_IDS" ]; then
        warn "No federated credentials found"
    else
        while IFS= read -r CRED_ID; do
            [ -z "$CRED_ID" ] && continue
            CRED_NAME=$(az ad app federated-credential show \
                --id "$APP_ID" \
                --federated-credential-id "$CRED_ID" \
                --query "name" --output tsv 2>/dev/null || echo "$CRED_ID")
            run az ad app federated-credential delete \
                --id "$APP_ID" \
                --federated-credential-id "$CRED_ID"
            success "Deleted federated credential: $CRED_NAME"
        done <<< "$CRED_IDS"
    fi

    # ── 3. Service Principal ──────────────────────────────────────────────────

    info "Service Principal for $APP_NAME"
    SP_OID=$(az ad sp show --id "$APP_ID" --query "id" --output tsv 2>/dev/null || true)
    if [ -n "$SP_OID" ] && [ "$SP_OID" != "None" ]; then
        run az ad sp delete --id "$SP_OID"
        success "Deleted service principal (Object ID: $SP_OID)"
    else
        warn "Service principal not found — skipping"
    fi

    # ── 4. App Registration ───────────────────────────────────────────────────

    info "App Registration deletion"
    run az ad app delete --id "$APP_ID"
    success "Deleted app registration (Client ID: $APP_ID)"
fi

# ── 5. Resource group (AKS + MC_* node group + ACR + Event Hubs) ─────────────

info "Resource group: $RESOURCE_GROUP"
if ! az group show --name "$RESOURCE_GROUP" &>/dev/null; then
    warn "Resource group not found — skipping"
else
    echo "    Contents: AKS cluster, ACR, Event Hubs namespace + hubs,"
    echo "              consumer groups, role assignments"
    echo "    The MC_* managed node resource group is deleted automatically."

    if [ "$ASYNC" = "1" ]; then
        run az group delete \
            --name "$RESOURCE_GROUP" \
            --yes \
            --no-wait
        if [ "$DRY_RUN" != "1" ]; then
            success "Deletion initiated asynchronously"
            echo "    Monitor:  az group show -n $RESOURCE_GROUP --query 'properties.provisioningState'"
        fi
    else
        echo "    Waiting for deletion to complete (this takes 5-15 minutes)..."
        run az group delete \
            --name "$RESOURCE_GROUP" \
            --yes
        if [ "$DRY_RUN" != "1" ]; then
            success "Resource group deleted"
        fi
    fi
fi

# ── 6. Verify ─────────────────────────────────────────────────────────────────

if [ "$DRY_RUN" != "1" ] && [ "$ASYNC" != "1" ]; then
    info "Verification"

    if az group show --name "$RESOURCE_GROUP" &>/dev/null; then
        warn "Resource group $RESOURCE_GROUP still exists — check portal"
    else
        success "Resource group $RESOURCE_GROUP: gone"
    fi

    REMAINING_APP=$(az ad app list --display-name "$APP_NAME" --query "[0].appId" --output tsv 2>/dev/null || true)
    if [ -n "$REMAINING_APP" ] && [ "$REMAINING_APP" != "None" ]; then
        warn "App registration $APP_NAME still exists (Client ID: $REMAINING_APP)"
    else
        success "App registration $APP_NAME: gone"
    fi
fi

# ── Done ──────────────────────────────────────────────────────────────────────

echo ""
echo "════════════════════════════════════════════════════════════════"
if [ "$DRY_RUN" = "1" ]; then
    echo " Dry run complete — nothing was deleted"
elif [ "$ASYNC" = "1" ]; then
    echo " Cleanup initiated (resource group deletion running async)"
    echo ""
    echo " Check status:"
    echo "   az group show --name $RESOURCE_GROUP --query 'properties.provisioningState'"
    echo ""
    echo " Verify complete:"
    echo "   az group list --query \"[?name=='$RESOURCE_GROUP']\""
    echo "   az ad app list --display-name '$APP_NAME'"
else
    echo " Cleanup complete"
fi
echo "════════════════════════════════════════════════════════════════"
