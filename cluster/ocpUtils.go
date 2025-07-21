package ocp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	providerName       = "ova-provider-test"
	secretName         = "ova-provider-lbmst"
	migrationName      = "hyperv-demo"
	planName           = "ovatohyper"
	sourceProviderType = "host"
	networkMapName     = "ova-network-map"
	storageMapName     = "ova-storage-map"
	sourceNetworkName  = "Network Adapter"
	destStorageClass   = "nfs-csi"
	destNetworkType    = "pod"
)

type PlanStatus struct {
	Phase      string `json:"phase"`
	Conditions []struct {
		Type    string `json:"type"`
		Status  string `json:"status"`
		Reason  string `json:"reason,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"conditions"`
}

type MigrationStatus struct {
	Phase      string `json:"phase"`
	Conditions []struct {
		Type    string `json:"type"`
		Status  string `json:"status"`
		Reason  string `json:"reason,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"conditions"`
}

type Migration struct {
	Status MigrationStatus `json:"status"`
}
type Plan struct {
	Status PlanStatus `json:"status"`
}

func WaitForPlanReady(namespace, planName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for plan %s to be ready", planName)
		case <-ticker.C:
			plan, err := getPlan(namespace, planName)
			if err != nil {
				return fmt.Errorf("failed to get plan: %w", err)
			}

			if isPlanReady(plan) {
				return nil
			}
			fmt.Println("Plan not ready yet, waiting...")
		}
	}
}

// Use kubectl to fetch the plan
func getPlan(namespace, name string) (*Plan, error) {
	cmd := exec.Command("kubectl", "get", "plan", name, "-n", namespace, "-o", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var plan Plan
	if err := json.Unmarshal(out, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

func isPlanReady(plan *Plan) bool {
	// Check phase or conditions for readiness, for example:
	if plan.Status.Phase == "Ready" {
		return true
	}
	for _, cond := range plan.Status.Conditions {
		if cond.Type == "Ready" && cond.Status == "True" {
			return true
		}
	}
	return false
}

func applyYamlFile(filename string) error {
	cmd := exec.Command("kubectl", "apply", "-f", filename)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr // Optional: include stdout for debugging too

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("kubectl apply failed: %v\nOutput:\n%s", err, stderr.String())
	}
	return nil
}

func applyYaml(filename string) error {
	cmd := exec.Command("oc", "apply", "-f", filename)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writeTemplateToFile(templateName, templateContent string, data any, filename string) error {
	tmpl, err := template.New(templateName).Parse(templateContent)
	if err != nil {
		return err
	}

	file, err := os.Create(filename)

	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, data)
}

func createOvaProviderYaml(
	namespace, providerName, secretName, secretNamespace, nfsURL, filename string,
) error {
	data := OvaProviderData{
		Namespace:       namespace,
		ProviderName:    providerName,
		SecretName:      secretName,
		SecretNamespace: secretNamespace,
		NFSURL:          nfsURL,
	}

	return writeTemplateToFile("ovaProvider", ovaProviderTemplate, data, filename)
}

func createMigrationPlanYaml(
	namespace, planName, sourceProvider, destProvider, networkMap, storageMap, vmID, vmName, filename string,
) error {
	data := MigrationPlanData{
		Namespace:      namespace,
		PlanName:       planName,
		SourceProvider: sourceProvider,
		DestProvider:   destProvider,
		NetworkMap:     networkMap,
		StorageMap:     storageMap,
		VMID:           vmID,
		VMName:         vmName,
	}

	return writeTemplateToFile("migrationPlan", migrationPlanTemplate, data, filename)
}

func createMigrationYaml(
	filename string,
	migrationName string,
	namespace string,
	planName string,
	planNamespace string,
) error {
	data := MigrationData{
		MigrationName: migrationName,
		Namespace:     namespace,
		PlanName:      planName,
		PlanNamespace: planNamespace,
	}

	return writeTemplateToFile("migration", migrationTemplate, data, filename)
}

func createStorageMapYaml(
	filename string,
	mapName string,
	namespace string,
	sourceProvider string,
	destinationProvider string,
	sourceStorageID string,
	destinationStorageClass string,
) error {
	data := StorageMapData{
		MapName:                 mapName,
		Namespace:               namespace,
		SourceProvider:          sourceProvider,
		DestinationProvider:     destinationProvider,
		SourceStorageID:         sourceStorageID,
		DestinationStorageClass: destinationStorageClass,
	}

	return writeTemplateToFile("storageMap", storageMapTemplate, data, filename)
}

func createNetworkMapYaml(
	filename string,
	mapName string,
	namespace string,
	sourceProvider string,
	destinationProvider string,
	sourceNetworkID string,
	sourceNetworkName string,
	destinationType string,
) error {
	data := NetworkMapData{
		MapName:             mapName,
		Namespace:           namespace,
		SourceProvider:      sourceProvider,
		DestinationProvider: destinationProvider,
		SourceNetworkID:     sourceNetworkID,
		SourceNetworkName:   sourceNetworkName,
		DestinationType:     destinationType,
	}

	return writeTemplateToFile("networkMap", networkMapTemplate, data, filename)

}

func createOvaSecretYaml(secretName, namespace, url string, insecureSkipVerify bool, filename string) error {
	secretData := SecretData{
		SecretName:               secretName,
		Namespace:                namespace,
		UrlBase64:                base64.StdEncoding.EncodeToString([]byte(url)),
		InsecureSkipVerifyBase64: base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%t", insecureSkipVerify))),
	}

	return writeTemplateToFile("secret", secretTemplate, secretData, filename)
}

func waitForMigrationComplete(namespace, migrationName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second) // poll interval
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for migration %s to complete", migrationName)
		case <-ticker.C:
			migration, err := getMigration(namespace, migrationName) // your function to fetch migration CR
			if err != nil {
				return fmt.Errorf("failed to get migration: %w", err)
			}

			if isMigrationSucceeded(migration) {
				fmt.Println("Migration succeeded!")
				return nil
			}
			if isMigrationFailed(migration) {
				return fmt.Errorf("migration failed")
			}

			progress := extractProgressPercentage(migration)
			fmt.Println(progress)
		}
	}
}

func extractProgressPercentage(migration *unstructured.Unstructured) string {
	vms, found, err := unstructured.NestedSlice(migration.Object, "status", "vms")
	if err != nil || !found || len(vms) == 0 {
		return "No VMs found in migration status"
	}

	var sb strings.Builder
	for _, v := range vms {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}

		name, _, _ := unstructured.NestedString(vm, "name")
		phase, _, _ := unstructured.NestedString(vm, "phase")
		pipeline, found, _ := unstructured.NestedSlice(vm, "pipeline")

		sb.WriteString(fmt.Sprintf("🖥️ VM: %s\n", name))
		sb.WriteString(fmt.Sprintf("   Phase: %s\n", phase))

		if found {
			for _, step := range pipeline {
				stepMap, ok := step.(map[string]interface{})
				if !ok {
					continue
				}

				stepName, _, _ := unstructured.NestedString(stepMap, "name")
				stepPhase, _, _ := unstructured.NestedString(stepMap, "phase")
				progressMap, _, _ := unstructured.NestedMap(stepMap, "progress")

				completed, _, _ := unstructured.NestedInt64(progressMap, "completed")
				total, _, _ := unstructured.NestedInt64(progressMap, "total")

				percentage := "?"
				if total > 0 {
					percentage = fmt.Sprintf("%d%%", int((completed*100)/total))
				}
				if stepPhase == "Completed" {
					percentage = "100%"
				}
				sb.WriteString(fmt.Sprintf("  Step: %s | %s | Progress: %s\n", stepName, stepPhase, percentage))
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func toInt64(val interface{}) (int64, bool) {
	switch v := val.(type) {
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		if err == nil {
			return i, true
		}
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return i, true
		}
	}
	return 0, false
}

func getMigration(namespace, migrationName string) (*unstructured.Unstructured, error) {
	var config *rest.Config
	var err error

	// Try loading from KUBECONFIG or default kubeconfig path
	kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	if _, err := os.Stat(kubeconfig); err == nil {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	migrationGVR := schema.GroupVersionResource{
		Group:    "forklift.konveyor.io",
		Version:  "v1beta1",
		Resource: "migrations",
	}

	migration, err := dynClient.Resource(migrationGVR).Namespace(namespace).Get(context.TODO(), migrationName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get migration CR: %w", err)
	}

	return migration, nil
}

func isMigrationSucceeded(migration *unstructured.Unstructured) bool {
	status, found, err := unstructured.NestedMap(migration.Object, "status")
	if !found || err != nil {
		return false
	}

	conditions, found, err := unstructured.NestedSlice(status, "conditions")
	if !found || err != nil {
		return false
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if cond["type"] == "Succeeded" && cond["status"] == "True" {
			return true
		}
	}
	return false
}

func isMigrationFailed(migration *unstructured.Unstructured) bool {
	status, found, err := unstructured.NestedMap(migration.Object, "status")
	if !found || err != nil {
		return false
	}

	conditions, found, err := unstructured.NestedSlice(status, "conditions")
	if !found || err != nil {
		return false
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if cond["type"] == "Failed" && cond["status"] == "True" {
			return true
		}
	}
	return false
}

func RunOvaMigration(vmName, outputDir string) error {
	namespace := os.Getenv("NAMESPACE")
	secretNamespace := namespace
	nfsURL := os.Getenv("OVA_PROVIDER_NFS_SERVER_PATH")
	// Use regenrated IDs for the sake of this example
	sourceStorageID := "2064f8686d4d7bbc79c201ea82518f263baa"
	sourceNetworkID := "d722072e029481b6ca769f17e8fc112a9f30"
	vmID := "42a55f0071494abc8e598aa681d1e821f73b"

	ovaProviderYaml := filepath.Join(outputDir, "ova-provider.yaml")
	storageMapYaml := filepath.Join(outputDir, "storage-map.yaml")
	networkMapYaml := filepath.Join(outputDir, "network-map.yaml")
	migrationPlanYaml := filepath.Join(outputDir, "plan.yaml")
	migrationYaml := filepath.Join(outputDir, "migration.yaml")
	secretYaml := filepath.Join(outputDir, "ova-secret.yaml")

	if namespace == "" {
		return fmt.Errorf("NAMESPACE environment variable not set")
	}
	if nfsURL == "" {
		return fmt.Errorf("OVA_PROVIDER_NFS_SERVER_PATH environment variable not set")
	}

	if err := createOvaSecretYaml(secretName, secretNamespace, nfsURL, false, secretYaml); err != nil {
		return fmt.Errorf("failed to create secret YAML: %w", err)
	}
	if err := applyYaml(secretYaml); err != nil {
		return fmt.Errorf("failed to apply secret YAML: %w", err)
	}

	if err := createStorageMapYaml(storageMapYaml, storageMapName, namespace, providerName, sourceProviderType, sourceStorageID, destStorageClass); err != nil {
		return fmt.Errorf("failed to create storage map YAML: %w", err)
	}
	if err := applyYamlFile(storageMapYaml); err != nil {
		return fmt.Errorf("failed to apply storage map YAML: %w", err)
	}

	if err := createNetworkMapYaml(networkMapYaml, networkMapName, namespace, providerName, sourceProviderType, sourceNetworkID, sourceNetworkName, destNetworkType); err != nil {
		return fmt.Errorf("failed to create network map YAML: %w", err)
	}
	if err := applyYamlFile(networkMapYaml); err != nil {
		return fmt.Errorf("failed to apply network map YAML: %w", err)
	}

	if err := createOvaProviderYaml(namespace, providerName, secretName, secretNamespace, nfsURL, ovaProviderYaml); err != nil {
		return fmt.Errorf("failed to create provider YAML: %w", err)
	}
	if err := applyYaml(ovaProviderYaml); err != nil {
		return fmt.Errorf("failed to apply provider YAML: %w", err)
	}

	time.Sleep(15 * time.Second) // make sure the provider is ready

	if err := createMigrationPlanYaml(namespace, planName, providerName, sourceProviderType, networkMapName, storageMapName, vmID, vmName, migrationPlanYaml); err != nil {
		return fmt.Errorf("failed to create migration plan YAML: %w", err)
	}
	if err := applyYamlFile(migrationPlanYaml); err != nil {
		return fmt.Errorf("failed to apply migration plan YAML: %w", err)
	}

	if err := createMigrationYaml(migrationYaml, migrationName, namespace, planName, namespace); err != nil {
		return fmt.Errorf("failed to create migration YAML: %w", err)
	}
	if err := applyYaml(migrationYaml); err != nil {
		return fmt.Errorf("failed to apply migration YAML: %w", err)
	}

	fmt.Printf("Waiting for migration %s to complete...\n", migrationName)
	timeout := 15 * time.Minute
	if err := waitForMigrationComplete(namespace, migrationName, timeout); err != nil {
		return fmt.Errorf("migration monitoring failed: %w", err)
	}

	fmt.Println("Migration completed successfully!")
	return nil
}
