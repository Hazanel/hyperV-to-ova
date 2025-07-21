package main

import (
	"encoding/json"
	"fmt"
	ocp "hyperv/cluster"
	hyperv "hyperv/common"
	osutil "hyperv/os"
	"hyperv/ova"
	"log"
	"os"
	"path/filepath"
	"sync"
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

const savejsonfile bool = false

func main() {

	connections, err := hyperv.LoadHyperVConnection()
	if err != nil {
		log.Fatalf("Connection setup failed: %v", err)
	}

	// Get vm list
	vmNames, err := hyperv.PerformVMAction(connections.Client, "", hyperv.ListVMs)
	if err != nil {
		log.Fatalf("Failed to list VMs: %v", err)
	}

	outputDir, err := filepath.Abs("output")
	if err != nil {
		log.Fatalf("Failed to get absolute path for output directory: %v", err)
	}

	// If "cmd" is in the path, remove it to get project root output
	if filepath.Base(filepath.Dir(outputDir)) == "cmd" {
		outputDir = filepath.Join(filepath.Dir(filepath.Dir(outputDir)), "output")
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("failed to create output directory: %v", err)
	}
	names := vmNames.([]string)

	var wg sync.WaitGroup
	for _, vmName := range names {
		wg.Add(1)

		go func(vmName string) {
			defer wg.Done()

			fmt.Printf("Fetching info for VM: %s\n", vmName)

			// Get VM info
			infoResult, err := hyperv.PerformVMAction(connections.Client, vmName, hyperv.GetVMInfo)
			if err != nil {
				log.Printf("Failed to get VM info: %v", err)
				return
			}

			vmInfoMap := infoResult.(map[string]interface{})

			// Extract dislk path from guest vm
			remotePath, _ := hyperv.ExtractPath(vmInfoMap)
			if remotePath == "" {
				log.Printf("No VHDX path found in VM data for %s", vmName)
				return
			}

			//Get guest OS info
			guestInfoJson, err := hyperv.GetGuestOSInfoFromVM(connections.Client, vmName, connections.User, connections.Password)
			if err != nil {
				log.Printf("VM '%s' may be OFF or unreachable: %v", vmName, err)
				return
			}

			guestOSMap, err := osutil.ParseGuestOSInfo(guestInfoJson)
			if err != nil {
				log.Printf("Failed to parse guest OS info for %s: %v", vmName, err)
				return
			}
			vmInfoMap["GuestOSInfo"] = guestOSMap

			// Perform VM action: shutdown
			fmt.Printf("Shutting down VM '%s'...\n", vmName)
			if _, err := hyperv.PerformVMAction(connections.Client, vmName, hyperv.Shutdown); err != nil {
				log.Printf("Failed to shut down VM %s: %v", vmName, err)
				return
			}

			if savejsonfile {
				jsonOut, _ := json.MarshalIndent(infoResult, "", "  ")
				if err := hyperv.SaveVMJsonToFile(jsonOut, vmName+"json"); err != nil {
					log.Printf("Failed to save JSON for %s: %v", vmName, err)
					return
				}
			}

			// Copy remote file disk with progress
			localFile := filepath.Join(outputDir, vmName+".vhdx")
			if err := hyperv.CopyRemoteFileWithProgress(connections.User, connections.Password,
				connections.HostIP, connections.SSHPort, remotePath, localFile); err != nil {
				log.Printf("SCP transfer failed for %s: %v", vmName, err)
				return
			}

			// Convert VHDX to raw format
			if err := hyperv.ConvertVHDXToRaw(localFile); err != nil {
				log.Printf("Failed to convert VHDX for %s: %v", vmName, err)
				return
			}

			// Format as OVA
			if err := ova.FormatFromHyperV(vmInfoMap, localFile); err != nil {
				log.Printf("Failed to format OVF for %s: %v", vmName, err)
				return
			}

		}(vmName) // capture loop variable
	}
	wg.Wait()
	fmt.Println("All VMs processed successfully.")

	if err := ocp.LoginToCluster(); err != nil {
		log.Fatalf("Cluster login failed: %v", err)
	}

	if err := ocp.RunOvaMigration(names[0], outputDir); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
}
