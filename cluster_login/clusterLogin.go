package clusterlogin

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func LoginToCluster() error {

	clusterName := os.Getenv("CLUSTER_NAME")
	if clusterName == "" {
		return fmt.Errorf("cluster name is required")
	}

	mountBasePath := os.Getenv("MOUNT_BASH_PATH")
	if mountBasePath == "" {
		return fmt.Errorf("mount base path is required")
	}
	nfsServerPath := os.Getenv("NFS_SERVER_PATH")
	if nfsServerPath == "" {
		return fmt.Errorf("NFS server path is required")
	}

	password, err := fetchClusterPassword(clusterName, mountBasePath, nfsServerPath)
	if err != nil {
		return fmt.Errorf("fetch password: %w", err)
	}

	if err := clusterLogin(clusterName, password); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	fmt.Printf("Logged in to cluster %s successfully.\n", clusterName)
	return nil
}

func fetchClusterPassword(clusterName, mountBasePath, nfsServerPath string) (string, error) {
	clusterMountPath := filepath.Join(mountBasePath, clusterName)

	// Ensure mount directory exists
	if _, err := os.Stat(mountBasePath); os.IsNotExist(err) {
		if err := runCommand("sudo", "mkdir", "-p", mountBasePath); err != nil {
			return "", fmt.Errorf("mkdir %s: %v", mountBasePath, err)
		}
	}

	// Mount if cluster path doesn't exist
	if _, err := os.Stat(clusterMountPath); os.IsNotExist(err) {
		if err := runCommand("sudo", "mount", "-t", "nfs", nfsServerPath, mountBasePath); err != nil {
			return "", fmt.Errorf("mount %s: %v", nfsServerPath, err)
		}
	}

	if _, err := os.Stat(clusterMountPath); os.IsNotExist(err) {
		return "", fmt.Errorf("cluster mount path %s does not exist after mount", clusterMountPath)
	}

	passwordFile := filepath.Join(clusterMountPath, "auth", "kubeadmin-password")
	content, err := os.ReadFile(passwordFile)
	if err != nil {
		return "", fmt.Errorf("read password file: %v", err)
	}

	return strings.TrimSpace(string(content)), nil
}

func clusterLogin(clusterName, password string) error {
	apiURL := fmt.Sprintf("https://api.%s.rhos-psi.cnv-qe.rhood.us:6443", clusterName)
	username := "kubeadmin"

	// Check if already logged in to same cluster
	if err := exec.Command("oc", "whoami").Run(); err == nil {
		cmd := exec.Command("oc", "whoami", "--show-server")
		out, err := cmd.Output()
		if err == nil && strings.Contains(string(out), clusterName) {
			fmt.Printf("Already logged in to %s\n", clusterName)
			return nil
		}
	}

	// Logout (ignore error)
	_ = exec.Command("oc", "logout").Run()

	// Attempt login
	cmd := exec.Command("oc", "login", "--insecure-skip-tls-verify=true", apiURL, "-u", username, "-p", password)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oc login: %v - %s", err, stderr.String())
	}

	return nil
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
