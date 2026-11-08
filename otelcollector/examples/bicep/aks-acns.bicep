// AKS cluster with Azure CNI Powered by Cilium and Advanced Container Networking
// Services (ACNS) enabled: network observability (metrics, Hubble flows, container
// network logs) and, optionally, Cilium FQDN / Layer 7 network policies.
//
// Deploy:
//   az deployment group create -g <rg> -f aks-acns.bicep -p clusterName=<name>
//
// After deployment, nothing is logged to /var/log/acns/hubble/events.log on the nodes until
// a ContainerNetworkLog resource exists; see hubble.md at the repo root.

@description('Name of the AKS cluster.')
param clusterName string

@description('Region of the cluster.')
param location string = resourceGroup().location

@description('Kubernetes version. Container network logs (stored mode) need 1.36 or later.')
param kubernetesVersion string = '1.37'

@description('Number of nodes in the system node pool.')
@minValue(1)
param systemNodeCount int = 2

@description('VM size of the system node pool.')
param systemNodeVmSize string = 'Standard_D4s_v5'

@description('ACNS security features: None = observability only; FQDN = DNS-based Cilium policies; L7 = FQDN plus HTTP/gRPC/Kafka policies (needed for HTTP details in flow logs).')
@allowed([
  'None'
  'FQDN'
  'L7'
])
param advancedNetworkPolicies string = 'FQDN'

@description('AKS pricing tier.')
@allowed([
  'Free'
  'Standard'
  'Premium'
])
param skuTier string = 'Standard'

resource aks 'Microsoft.ContainerService/managedClusters@2026-06-01' = {
  name: clusterName
  location: location
  identity: {
    type: 'SystemAssigned'
  }
  sku: {
    name: 'Base'
    tier: skuTier
  }
  properties: {
    dnsPrefix: clusterName
    kubernetesVersion: kubernetesVersion
    enableRBAC: true
    // Workload Identity: lets the collector use the azure_auth extension without secrets
    oidcIssuerProfile: {
      enabled: true
    }
    securityProfile: {
      workloadIdentity: {
        enabled: true
      }
    }
    agentPoolProfiles: [
      {
        name: 'system'
        mode: 'System'
        type: 'VirtualMachineScaleSets'
        count: systemNodeCount
        vmSize: systemNodeVmSize
        osType: 'Linux'
        osSKU: 'AzureLinux'
      }
    ]
    networkProfile: {
      // Azure CNI Powered by Cilium (overlay). ACNS stored flow logs and the
      // security features require the Cilium data plane.
      networkPlugin: 'azure'
      networkPluginMode: 'overlay'
      networkDataplane: 'cilium'
      networkPolicy: 'cilium'
      podCidr: '10.244.0.0/16'
      serviceCidr: '10.0.0.0/16'
      dnsServiceIP: '10.0.0.10'
      loadBalancerSku: 'standard'
      outboundType: 'loadBalancer'

      // Advanced Container Networking Services
      advancedNetworking: {
        enabled: true
        observability: {
          enabled: true // network metrics, Hubble, container network logs
        }
        security: advancedNetworkPolicies == 'None'
          ? {
              enabled: false
            }
          : {
              enabled: true
              advancedNetworkPolicies: advancedNetworkPolicies
            }
      }
    }
  }
}

output clusterId string = aks.id
output oidcIssuerUrl string = aks.properties.oidcIssuerProfile.issuerURL
