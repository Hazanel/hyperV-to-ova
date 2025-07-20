package main

import (
	"encoding/json"
	"fmt"
	hyperv "hyperv/common"
	osutil "hyperv/os"
	"hyperv/ova"
	"log"
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
	client, hostIP, sshPort, user, password, err := hyperv.LoadHyperVConnection()
	if err != nil {
		log.Fatalf("Connection setup failed: %v", err)
	}

	vmNames, err := hyperv.PerformVMAction(client, "", hyperv.ListVMs)
	if err != nil {
		log.Fatalf("Failed to list VMs: %v", err)
	}

	names := vmNames.([]string)

	for _, vmName := range names {
		fmt.Printf("Fetching info for VM: %s\n", vmName)

		// 2. Fetch full VM info
		infoResult, err := hyperv.PerformVMAction(client, vmName, hyperv.GetVMInfo)
		if err != nil {
			log.Printf("Failed to get VM info: %v", err)
			continue
		}

		vmInfoMap := infoResult.(map[string]interface{})

		// 3. Extract VHDX path
		remotePath, _ := hyperv.ExtractPath(vmInfoMap)
		if remotePath == "" {
			log.Fatalf("No VHDX path found in VM data")
		}

		// 4. Get Guest OS Info
		guestInfoJson, err := hyperv.GetGuestOSInfoFromVM(client, vmName, user, password)
		if err != nil {
			log.Printf("VM '%s' may be OFF or unreachable: %v", vmName, err)
			continue
		}
		guestOSMap, err := osutil.ParseGuestOSInfo(guestInfoJson)
		if err != nil {
			log.Fatalf("Failed to parse guest OS info: %v", err)
		}
		vmInfoMap["GuestOSInfo"] = guestOSMap

		// 5. Shutdown VM
		fmt.Printf("Shutting down VM '%s'...\n", vmName)
		if _, err := hyperv.PerformVMAction(client, vmName, hyperv.Shutdown); err != nil {
			log.Fatalf("Failed to shut down VM: %v", err)
		}

		if savejsonfile {
			jsonOut, _ := json.MarshalIndent(infoResult, "", "  ")

			if err := hyperv.SaveVMJsonToFile(jsonOut, vmName+"json"); err != nil {
				log.Fatalf("%v", err)
			}
		}

		localFile := vmName + ".vhdx"

		if hyperv.CopyRemoteFileWithProgress(user, password, hostIP, sshPort, remotePath, vmName+".vhdx") != nil {
			log.Fatalf("SCP transfer failed: %v", err)
		}

		// 7. Convert VHDX to RAW
		if hyperv.ConvertVHDXToRaw(localFile) != nil {
			log.Fatalf("Failed to convert VHDX to RAW: %v", err)
		}
		// 8. Generate OVF
		if ova.FormatFromHyperV(vmInfoMap, localFile) != nil {
			log.Fatalf("Failed to format OVF from HyperV VM: %v", err)
		}

	}
}
