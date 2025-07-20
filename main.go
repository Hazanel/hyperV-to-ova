package main

import (
	"encoding/json"
	"fmt"
	ocp "hyperv/cluster"
	hyperv "hyperv/common"
	osutil "hyperv/os"
	"hyperv/ova"
	"log"
	"time"
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

	if err := ocp.LoginToCluster(); err != nil {
		log.Fatalf("Cluster login failed: %v", err)
	}
	namespace := "openshift-mtv"
	providerName := "ova-provider-test"
	secretName := "ova-provider-bzhf8"
	secretNamespace := "openshift-mtv"
	nfsURL := "10.8.3.97:/srv/www/html/v2v-image/mtv/elad"
	yamlFile := "ova-provider.yaml"
	migrationName := "hyperv-demo"

	err = ocp.CreateOvaProviderYaml(namespace, providerName, secretName, secretNamespace, nfsURL, yamlFile)
	if err != nil {
		log.Fatalf("Failed to create Provider yaml: %v", err)
	}

	// Apply the YAML
	if err := ocp.ApplyYaml(yamlFile); err != nil {
		log.Fatalf("Failed to apply Provider yaml: %v", err)
	}

	err = ocp.CreateStorageMapYaml(
		"storage-map.yaml",
		"ova-storage-map",
		"openshift-mtv",
		"ova-provider",
		"host",
		"2064f8686d4d7bbc79c201ea82518f263baa", // source ID
		"nfs-csi",                              // dest SC
	)
	if err != nil {
		log.Fatalf("failed to write storage map: %v", err)
	}

	if err := ocp.ApplyYamlFile("storage-map.yaml"); err != nil {
		log.Fatalf("failed to apply storage map: %v", err)
	}
	err = ocp.CreateNetworkMapYaml(
		"network-map.yaml",
		"ova-network-map",
		"openshift-mtv",
		"ova-provider",
		"host",
		"d722072e029481b6ca769f17e8fc112a9f30", // source network ID
		"Network Adapter",                      // source network name
		"pod",                                  // destination type: pod, multus, etc.
	)
	if err != nil {
		log.Fatalf("failed to write network map: %v", err)
	}
	if err = ocp.ApplyYamlFile("network-map.yaml"); err != nil {
		log.Fatalf("failed to apply network map: %v", err)
	}
	err = ocp.CreateMigrationPlanYaml(
		"openshift-mtv",
		"ovatohyper",
		"ova-provider",
		"host",
		"ova-network-map",
		"ova-storage-map",
		"42a55f0071494abc8e598aa681d1e821f73b",
		"v-2019",
		"plan.yaml",
	)
	err = ocp.ApplyYamlFile("plan.yaml")
	if err != nil {
		fmt.Printf("Failed to apply plan: %v\n", err)
	}

	migrationYaml := "migration.yaml"
	err = ocp.CreateMigrationYaml(migrationYaml, "hyperv-demo", "openshift-mtv", "ovatohyper", "openshift-mtv")
	if err != nil {
		log.Fatalf("Failed to create migration yaml: %v", err)
	}
	if err = ocp.ApplyYaml(migrationYaml); err != nil {
		log.Fatalf("Failed to apply migration yaml: %v", err)
	}

	timeout := 30 * time.Minute

	fmt.Printf("Waiting for migration %s to complete...\n", migrationName)
	if err := ocp.WaitForMigrationComplete(namespace, migrationName, timeout); err != nil {
		log.Fatalf("Migration monitoring failed: %v", err)
	}

	fmt.Println("Migration completed successfully!")
}
