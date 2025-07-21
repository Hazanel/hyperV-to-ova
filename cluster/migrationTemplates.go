package ocp

const networkMapTemplate = `apiVersion: forklift.konveyor.io/v1beta1
kind: NetworkMap
metadata:
  name: {{.MapName}}
  namespace: {{.Namespace}}
spec:
  map:
    - source:
        id: {{.SourceNetworkID}}
        name: {{.SourceNetworkName}}
      destination:
        type: {{.DestinationType}}
  provider:
    source:
      name: {{.SourceProvider}}
      namespace: {{.Namespace}}
    destination:
      name: {{.DestinationProvider}}
      namespace: {{.Namespace}}
`

const storageMapTemplate = `apiVersion: forklift.konveyor.io/v1beta1
kind: StorageMap
metadata:
  name: {{.MapName}}
  namespace: {{.Namespace}}
spec:
  map:
    - source:
        id: {{.SourceStorageID}}
      destination:
        storageClass: {{.DestinationStorageClass}}
  provider:
    source:
      name: {{.SourceProvider}}
      namespace: {{.Namespace}}
    destination:
      name: {{.DestinationProvider}}
      namespace: {{.Namespace}}
`
const migrationTemplate = `apiVersion: forklift.konveyor.io/v1beta1
kind: Migration
metadata:
  name: {{.MigrationName}}
  namespace: {{.Namespace}}
spec:
  plan:
    name: {{.PlanName}}
    namespace: {{.PlanNamespace}}
`

const migrationPlanTemplate = `apiVersion: forklift.konveyor.io/v1beta1
kind: Plan
metadata:
  name: {{.PlanName}}
  namespace: {{.Namespace}}
spec:
  provider:
    source:
      apiVersion: forklift.konveyor.io/v1beta1
      kind: Provider
      name: {{.SourceProvider}}
      namespace: {{.Namespace}}
    destination:
      apiVersion: forklift.konveyor.io/v1beta1
      kind: Provider
      name: {{.DestProvider}}
      namespace: {{.Namespace}}
  map:
    network:
      apiVersion: forklift.konveyor.io/v1beta1
      kind: NetworkMap
      name: {{.NetworkMap}}
      namespace: {{.Namespace}}
    storage:
      apiVersion: forklift.konveyor.io/v1beta1
      kind: StorageMap
      name: {{.StorageMap}}
      namespace: {{.Namespace}}
  targetNamespace: {{.Namespace}}
  pvcNameTemplateUseGenerateName: true
  skipGuestConversion: false
  warm: false
  migrateSharedDisks: true
  vms:
    - id: {{.VMID}}
      name: {{.VMName}}
`

const ovaProviderTemplate = `apiVersion: forklift.konveyor.io/v1beta1
kind: Provider
metadata:
  name: {{.ProviderName}}
  namespace: {{.Namespace}}
spec:
  secret:
    name: {{.SecretName}}
    namespace: {{.SecretNamespace}}
  type: ova
  url: '{{.NFSURL}}'
`

const secretTemplate = `apiVersion: v1
kind: Secret
metadata:
  name: {{.SecretName}}
  namespace: {{.Namespace}}
  labels:
    createdForProviderType: ova
    createdForResourceType: providers
type: Opaque
data:
  url: {{.UrlBase64}}
  insecureSkipVerify: {{.InsecureSkipVerifyBase64}}
`

type SecretData struct {
	SecretName               string
	Namespace                string
	UrlBase64                string
	InsecureSkipVerifyBase64 string
}

type OvaProviderData struct {
	Namespace       string
	ProviderName    string
	SecretName      string
	SecretNamespace string
	NFSURL          string
}

type MigrationPlanData struct {
	Namespace      string
	PlanName       string
	SourceProvider string
	DestProvider   string
	NetworkMap     string
	StorageMap     string
	VMID           string
	VMName         string
}

type MigrationData struct {
	MigrationName string
	Namespace     string
	PlanName      string
	PlanNamespace string
}

type StorageMapData struct {
	MapName                 string
	Namespace               string
	SourceProvider          string
	DestinationProvider     string
	SourceStorageID         string
	DestinationStorageClass string
}

type NetworkMapData struct {
	MapName             string
	Namespace           string
	SourceProvider      string
	DestinationProvider string
	SourceNetworkID     string
	SourceNetworkName   string
	DestinationType     string
}
