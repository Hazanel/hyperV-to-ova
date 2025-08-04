package main

import (
	"encoding/json"
	"fmt"
	ocp "hyperv/cluster"
	hyperv "hyperv/common"
	nfs "hyperv/nfs"
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

func main() {

	//Detect special flag to only run the CopyFilesNfsServer (under sudo)
	if len(os.Args) >= 4 && os.Args[1] == "--copy-files" {
		fmt.Println("ARGS:", os.Args)
		srcDir := os.Args[2]
		dstDir := os.Args[3]
		if err := nfs.CopyFilesNfsServer(srcDir, dstDir); err != nil {
			log.Fatalf("Copy failed: %v", err)
		}
		os.Exit(0)
	}
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

			// Extract disk path from guest vm
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

			//If you want to save the VM info to a file, set the SAVE_VM_INFO environment variable to true
			if os.Getenv("SAVE_VM_INFO") == "true" {
				jsonOut, _ := json.MarshalIndent(infoResult, "", "  ")
				if err := hyperv.SaveVMJsonToFile(jsonOut, filepath.Join(outputDir, vmName+".json")); err != nil {
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

			// Format as OVA
			if err := ova.FormatFromHyperV(vmInfoMap, localFile); err != nil {
				log.Printf("Failed to format OVF for %s: %v", vmName, err)
				return
			}

		}(vmName) // capture loop variable
	}
	wg.Wait()
	fmt.Println("All VMs processed successfully.")

	if hyperv.AskYesNo("Would you like to copy OVA files to the NFS server?") {
		if err := nfs.CopyToNFSServer(outputDir); err != nil {
			log.Fatalf("Copy failed: %v", err)
		}
	} else {
		fmt.Println("Skipping copy to NFS server.")
	}

	if hyperv.AskYesNo("Would you like to create an  OVA provider and perform a migration?") {
		if err := ocp.LoginToCluster(); err != nil {
			log.Fatalf("Cluster login failed: %v", err)
		}

		if err := ocp.RunOvaMigration(names[0], outputDir); err != nil {
			log.Fatalf("Migration failed: %v", err)
		}
	} else {
		fmt.Println("Skipping OVA provider creation and migration.")
	}
}
