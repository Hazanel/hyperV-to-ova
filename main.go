package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hyperv/ova"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bramvdbogaerde/go-scp"
	"github.com/bramvdbogaerde/go-scp/auth"
	"github.com/joho/godotenv"
	"github.com/masterzen/winrm"
	"golang.org/x/crypto/ssh"
)

//Make sure to have quemu installed:
// sudo dnf install qemu-img

// Note: Ensure WinRM is configured on the Windows VM with bellow power-shell commands
//winrm quickconfig
//Set-Item -Path WSMan:\localhost\Service\Auth\Basic -Value $true
//Set-Item -Path WSMan:\localhost\Service\AllowUnencrypted -Value $true
//Restart-Service WinRM

//SSH configuration
// # Install OpenSSH server (Windows Server 2019+)
// Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0

// # Start the service
// Start-Service sshd

// # Set it to start automatically
// Set-Service -Name sshd -StartupType 'Automatic'

// # Allow through firewall
// New-NetFirewallRule -Name sshd -DisplayName 'OpenSSH Server (sshd)' -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22

func GetGuestOSInfoFromVM(client *winrm.Client, vmName, guestUser, guestPassword string) (string, error) {
	psCmd := fmt.Sprintf(`powershell -Command "$secpasswd = ConvertTo-SecureString '%s' -AsPlainText -Force; $cred = New-Object System.Management.Automation.PSCredential('%s', $secpasswd); Invoke-Command -VMName '%s' -Credential $cred -ScriptBlock { Get-CimInstance Win32_OperatingSystem | Select Caption, Version, OSArchitecture } | ConvertTo-Json -Compress"`,
		guestPassword, guestUser, vmName)

	var stdout, stderr strings.Builder
	exitCode, err := client.Run(psCmd, &stdout, &stderr)
	if err != nil {
		return "", fmt.Errorf("failed to run command: %w\nstderr: %s", err, stderr.String())
	}
	if exitCode != 0 {
		return "", fmt.Errorf("non-zero exit code: %d\nstderr: %s", exitCode, stderr.String())
	}
	return stdout.String(), nil
}

const savejsonfile bool = false

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	// Get credentials
	user := os.Getenv("HYPERV_USER")
	password := os.Getenv("HYPERV_PASS")

	if user == "" || password == "" {
		log.Fatal("Missing credentials in environment")
	}
	host := "192.168.122.249:22" // SSH port 22
	endpoint := winrm.NewEndpoint("192.168.122.249", 5985, false, false, nil, nil, nil, 0)
	client, err := winrm.NewClient(endpoint, user, password)
	if err != nil {
		log.Fatalf("Failed to create WinRM client: %s", err)
	}

	// Step 1: Run PowerShell to get VM list as JSON
	vmInfoCommand := `powershell -Command "Get-VM | ConvertTo-Json -Depth 3"`
	//command := `powershell -Command "Get-VM | ForEach-Object { $vm = $_; $netAdapters = Get-VMNetworkAdapter -VMName $vm.Name -ErrorAction SilentlyContinue | ForEach-Object { [PSCustomObject]@{ MacAddress = $_.MacAddress; SwitchName = $_.SwitchName } }; $hardDrives = Get-VMHardDiskDrive -VMName $vm.Name -ErrorAction SilentlyContinue | ForEach-Object { [PSCustomObject]@{ Path = $_.Path; ControllerType = $_.ControllerType.ToString(); ControllerNumber = $_.ControllerNumber; ControllerLocation = $_.ControllerLocation } }; [PSCustomObject]@{ Name = $vm.Name; ID = $vm.Id.Guid; PowerState = $vm.State.ToString(); GuestOS = $vm.Guest.OperatingSystem; IpAddress = ($vm.NetworkAdapters[0].IpAddresses -join ','); HostName = $vm.ComputerName; Disks = $hardDrives; Networks = $netAdapters } } | ConvertTo-Json -Depth 5"`
	var stdout, stderr bytes.Buffer
	getVMexitCode, err := client.Run(vmInfoCommand, &stdout, &stderr)
	if err != nil {
		log.Fatalf("Command failed: %s\nSTDERR: %s\nSTDOUT: %s", err, stderr.String(), stdout.String())
	} else if getVMexitCode != 0 {
		log.Fatalf("Command exited with code %d: %s\nSTDERR: %s\nSTDOUT: %s", getVMexitCode, err, stderr.String(), stdout.String())
	}

	var vmInfoMap interface{}
	if err := json.Unmarshal(stdout.Bytes(), &vmInfoMap); err != nil {
		log.Fatalf("Failed to parse JSON: %s\nRaw Output:\n%s", err, stdout.String())
	}

	jsonOut, _ := json.MarshalIndent(vmInfoMap, "", "  ")
	//fmt.Println("Parsed JSON:\n", string(jsonOut))

	// Step 2: Extract VHDX path (assumes first VM, adjust if needed)
	var path string
	switch v := vmInfoMap.(type) {
	case map[string]interface{}: // single VM
		path, _ = extractPath(v)
	case []interface{}: // list of VMs
		if len(v) > 0 {
			if m, ok := v[0].(map[string]interface{}); ok {
				path, _ = extractPath(m)
			}
		}
	}
	if path == "" {
		log.Fatal("No VHDX path found in VM data")
	}
	fmt.Printf("VHDX path: %s\n", path)

	// Extract VM name from JSON (optional fallback)
	vmName := ""
	switch v := vmInfoMap.(type) {
	case map[string]interface{}:
		if name, ok := v["Name"].(string); ok {
			vmName = name
		}
	case []interface{}:
		if len(v) > 0 {
			if m, ok := v[0].(map[string]interface{}); ok {
				if name, ok := m["Name"].(string); ok {
					vmName = name
				}
			}
		}
	}
	if vmName == "" {
		log.Fatal("Failed to determine VM name from JSON")
	}

	guestOSJson, err := GetGuestOSInfoFromVM(client, vmName, user, password)
	if err != nil {
		fmt.Println("Error getting guest OS info:", err)
		return
	}

	fmt.Printf("Guest OS Info: %+v\n", guestOSJson)
	guestOSMap, err := ova.ParseGuestOSInfo(guestOSJson)
	if err != nil {
		log.Fatalf("Failed to parse guest OS info: %v", err)
	}

	// Type assert to map[string]interface{}
	vmMap, ok := vmInfoMap.(map[string]interface{})
	if !ok {
		log.Fatalf("expected JSON object (map[string]interface{}), got something else")
		return
	}
	vmMap["GuestOSInfo"] = guestOSMap

	// Stop the VM
	fmt.Printf("Shutting down VM '%s'...\n", vmName)
	shutdownCmd := fmt.Sprintf(`powershell -Command "Stop-VM -Name '%s' -Force -Confirm:$false"`, vmName)

	var stopOut, stopErr bytes.Buffer
	stopExitCode, err := client.Run(shutdownCmd, &stopOut, &stopErr)
	if err != nil {
		log.Fatalf("Failed to shut down VM: %v\nStderr: %s", err, stopErr.String())
	}
	fmt.Printf("Shutdown completed (exit code %d)\n", stopExitCode)

	// Create SCP client config using password auth
	clientConfig, err := auth.PasswordKey(user, password, ssh.InsecureIgnoreHostKey())
	if err != nil {
		log.Fatalf("Failed to create SSH client config: %v", err)
	}

	// Create new SCP client
	scpClient := scp.NewClient(host, &clientConfig)

	// Connect to the SSH server
	err = scpClient.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to SSH server: %v", err)
	}
	defer scpClient.Close()

	if savejsonfile {
		if err := saveVMJsonToFile(jsonOut, vmName+"json"); err != nil {
			log.Fatalf("%v", err)
		}
	}
	localFile := vmName + ".vhdx"

	// Create local file to write to
	f, err := os.Create(localFile)
	if err != nil {
		log.Fatalf("Failed to create local file: %v", err)
	}
	defer f.Close()

	// Download remote file via SCP
	done := make(chan struct{})

	go func(filename string) {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fi, err := os.Stat(filename)
				if err == nil {
					// Clear line by padding with spaces
					fmt.Printf("\rDownloading... %d bytes      ", fi.Size())
				}
			case <-done:
				fmt.Println("\nDownload completed.")
				return
			}
		}
	}(localFile)

	fmt.Println("Downloading VHDX file from remote server...")
	err = scpClient.CopyFromRemote(context.Background(), f, path)
	if err != nil {
		log.Fatalf("Failed to copy remote file: %v", err)
	}
	close(done) // stop the progress goroutine
	fmt.Println("\nFile downloaded successfully to", localFile)

	// Step 3: Convert  VHDX to raw
	cmd := exec.Command(
		"virt-v2v",
		"-i", "disk",
		localFile,
		"-o", "local",
		"-of", "raw",
		"-os", ".",
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Println("Converting VHDX to raw format...")
	if err := cmd.Run(); err != nil {
		log.Fatalf("VHDX ocnvrsion failed: %v", err)
	}

	fmt.Println("VHDX ocnvrsion completed successfully.")

	os.Rename(localFile+"-sda", removeFileExtension(localFile)+".raw")

	ovaFile, err := ova.FormatFromHyperV(vmMap)
	if err != nil {
		log.Fatalf("Failed to format OVF: %s", err)
	}
	if err := os.WriteFile(removeFileExtension(localFile)+".ovf", ovaFile, 0644); err != nil {
		log.Fatalf("Failed to write OVF file: %v", err)
	}
	fmt.Println("OVF file created successfully: vm-2019.ovf")

	// Make sure to be loged-in to the cluster before running this command
	cmd = exec.Command("./cluster-login.sh", "cluster-login", "qemtv-06")
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to log in to the cluster: %v", err)
	}

	//TBD - create an OVA provider and a plan to migrate raw file with ovf to openshift
	// Need either to set up an NFS server or use an existing one to store the raw file

	// cmd = exec.Command(
	// 	"virtctl", "image-upload", "pvc", "win2019-disk",
	// 	"--image-path=./vm-2019.vhdx-sda",
	// 	"--access-mode", "ReadWriteOnce",
	// 	"--size", "15Gi",
	// 	"--storage-class", "ocs-storagecluster-ceph-rbd",
	// 	"--namespace", "openshift-mtv",
	// 	"--insecure",
	// )

	// // Attach stdout/stderr to console for real-time output
	// cmd.Stdout = os.Stdout
	// cmd.Stderr = os.Stderr

	// log.Println("Uploading image to PVC...")
	// if err := cmd.Run(); err != nil {
	// 	log.Fatalf("virtctl image-upload failed: %v", err)
	// }
	// log.Println("Upload completed successfully.")
}

func extractPath(vm map[string]interface{}) (string, bool) {
	if drives, ok := vm["HardDrives"]; ok {
		if dlist, ok := drives.([]interface{}); ok && len(dlist) > 0 {
			if d, ok := dlist[0].(map[string]interface{}); ok {
				if p, ok := d["Path"].(string); ok {
					return p, true
				}
			}
		}
	}
	return "", false
}

func saveVMJsonToFile(jsonOut []byte, filename string) error {

	err := os.WriteFile(filename, jsonOut, 0644)
	if err != nil {
		return fmt.Errorf("failed to write JSON to file: %w", err)
	}
	fmt.Printf("JSON output saved to: %s\n", filename)
	return nil
}

func removeFileExtension(filename string) string {
	ext := filepath.Ext(filename)
	return strings.TrimSuffix(filename, ext)
}
