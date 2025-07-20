package ocp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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
			plan, err := GetPlan(namespace, planName)
			if err != nil {
				return fmt.Errorf("failed to get plan: %w", err)
			}

			if IsPlanReady(plan) {
				return nil
			}
			fmt.Println("Plan not ready yet, waiting...")
		}
	}
}

// Use kubectl to fetch the plan
func GetPlan(namespace, name string) (*Plan, error) {
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

func IsPlanReady(plan *Plan) bool {
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

func CreateOvaProviderYaml(
	namespace, providerName, secretName, secretNamespace, nfsURL, filename string,
) error {
	content := fmt.Sprintf(`apiVersion: forklift.konveyor.io/v1beta1
kind: Provider
metadata:
  name: %s
  namespace: %s
spec:
  secret:
    name: %s
    namespace: %s
  type: ova
  url: '%s'
`, providerName, namespace, secretName, secretNamespace, nfsURL)

	return os.WriteFile(filename, []byte(content), 0644)
}

func CreateMigrationPlanYaml(
	namespace, planName, sourceProvider, destProvider, networkMap, storageMap, vmID, vmName, filename string,
) error {
	content := fmt.Sprintf(`apiVersion: forklift.konveyor.io/v1beta1
kind: Plan
metadata:
  name: %s
  namespace: %s
spec:
  provider:
    source:
      apiVersion: forklift.konveyor.io/v1beta1
      kind: Provider
      name: %s
      namespace: %s
    destination:
      apiVersion: forklift.konveyor.io/v1beta1
      kind: Provider
      name: %s
      namespace: %s
  map:
    network:
      apiVersion: forklift.konveyor.io/v1beta1
      kind: NetworkMap
      name: %s
      namespace: %s
    storage:
      apiVersion: forklift.konveyor.io/v1beta1
      kind: StorageMap
      name: %s
      namespace: %s
  targetNamespace: %s
  pvcNameTemplateUseGenerateName: true
  skipGuestConversion: false
  warm: false
  migrateSharedDisks: true
  vms:
    - id: %s
      name: %s
`, planName, namespace,
		sourceProvider, namespace,
		destProvider, namespace,
		networkMap, namespace,
		storageMap, namespace,
		namespace,
		vmID, vmName)

	return os.WriteFile(filename, []byte(content), 0644)
}

func runMigrationAndWait(namespace, migrationName string, timeout time.Duration) error {
	err := ApplyYaml(fmt.Sprintf("/tmp/%s.yaml", migrationName)) // your helper to kubectl apply migration yaml
	if err != nil {
		return fmt.Errorf("failed to apply migration: %w", err)
	}
	return WaitForMigrationComplete(namespace, migrationName, timeout) // your existing function
}
func CreateMigrationYaml(filename, migrationName, namespace, planName, planNamespace string) error {
	content := fmt.Sprintf(`apiVersion: forklift.konveyor.io/v1beta1
kind: Migration
metadata:
  name: %s
  namespace: %s
spec:
  plan:
    name: %s
    namespace: %s
`, migrationName, namespace, planName, planNamespace)

	return os.WriteFile(filename, []byte(content), 0644)
}

func ApplyYamlFile(filename string) error {
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

func ApplyYaml(filename string) error {
	cmd := exec.Command("oc", "apply", "-f", filename)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func CreateStorageMapYaml(
	filename string,
	mapName string,
	namespace string,
	sourceProvider string,
	destinationProvider string,
	sourceStorageID string,
	destinationStorageClass string,
) error {
	content := fmt.Sprintf(`apiVersion: forklift.konveyor.io/v1beta1
kind: StorageMap
metadata:
  name: %s
  namespace: %s
spec:
  map:
    - source:
        id: %s
      destination:
        storageClass: %s
  provider:
    source:
      name: %s
      namespace: %s
    destination:
      name: %s
      namespace: %s
`, mapName, namespace, sourceStorageID, destinationStorageClass,
		sourceProvider, namespace, destinationProvider, namespace)

	return os.WriteFile(filename, []byte(content), 0644)
}

func CreateNetworkMapYaml(
	filename string,
	mapName string,
	namespace string,
	sourceProvider string,
	destinationProvider string,
	sourceNetworkID string,
	sourceNetworkName string,
	destinationType string,
) error {
	content := fmt.Sprintf(`apiVersion: forklift.konveyor.io/v1beta1
kind: NetworkMap
metadata:
  name: %s
  namespace: %s
spec:
  map:
    - source:
        id: %s
        name: %s
      destination:
        type: %s
  provider:
    source:
      name: %s
      namespace: %s
    destination:
      name: %s
      namespace: %s
`, mapName, namespace, sourceNetworkID, sourceNetworkName, destinationType,
		sourceProvider, namespace, destinationProvider, namespace)

	return os.WriteFile(filename, []byte(content), 0644)
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

func WaitForMigrationComplete(namespace, migrationName string, timeout time.Duration) error {
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
		return "⚠️ No VMs found in migration status"
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
