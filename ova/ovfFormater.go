package ova

import (
	"encoding/xml"
	"fmt"
	hyperv "hyperv/common"
	osutil "hyperv/os"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GetOVFOperatingSystemID returns the OVF OS ID for a given OS name string.
func GetOVFOperatingSystemID(osName string) int {
	// Normalize input to lowercase for exact match
	key := strings.ToLower(osName)

	// Exact match
	if id, found := osutil.OsNameToID[key]; found {
		return id
	}

	// Fallback: partial match
	for known, id := range osutil.OsNameToID {
		if strings.Contains(key, strings.ToLower(known)) {
			return id
		}
	}

	// Final fallback
	return 1 // Other
}

func FormatFromHyperV(vm interface{}, remptePath string) error {

	vmMap, ok := vm.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid VM format: expected map[string]interface{}")
	}

	var (
		files          []File
		disks          []Disk
		networks       []Network
		hardwareItems  []Item
		itemInstanceID = 1
	)

	// --- CPU ---
	cpuCount := int64(1)
	if val, ok := vmMap["ProcessorCount"].(float64); ok {
		cpuCount = int64(val)
	}
	hardwareItems = append(hardwareItems, Item{
		InstanceID:      strconv.Itoa(itemInstanceID),
		ResourceType:    3,
		Description:     "Number of virtual CPUs",
		AllocationUnits: "hertz * 10^6",
		ElementName:     fmt.Sprintf("%d virtual CPU(s)", cpuCount),
		VirtualQuantity: cpuCount,
	})
	itemInstanceID++

	// --- Memory ---
	memoryMB := int64(1024)
	if val, ok := vmMap["MemoryStartup"].(float64); ok {
		memoryMB = int64(val / 1024 / 1024)
	}
	hardwareItems = append(hardwareItems, Item{
		InstanceID:      strconv.Itoa(itemInstanceID),
		ResourceType:    4,
		Description:     "Memory Size",
		AllocationUnits: "byte * 2^20",
		ElementName:     fmt.Sprintf("%dMB of memory", memoryMB),
		VirtualQuantity: memoryMB,
	})
	itemInstanceID++

	// --- IDE Controller ---
	ideControllerID := strconv.Itoa(itemInstanceID)
	hardwareItems = append(hardwareItems, Item{
		InstanceID:   ideControllerID,
		ResourceType: 5,
		Address:      "0",
		Description:  "IDE Controller",
		ElementName:  "VirtualIDEController 0",
	})
	itemInstanceID++

	// --- Hard Disks ---
	if hdList, ok := vmMap["HardDrives"].([]interface{}); ok {
		for i := range hdList {

			diskIndex := i + 1
			fileRefID := fmt.Sprintf("file%d", diskIndex)

			rawDiskPath := hyperv.RemoveFileExtension(remptePath) + ".raw"
			fileName := filepath.Base(rawDiskPath)
			diskCapacity := int64(10 * 1024 * 1024 * 1024) // fallback size
			if stat, err := os.Stat(rawDiskPath); err == nil {
				diskCapacity = stat.Size()
			} else {
				return fmt.Errorf("failed to get size of raw disk file %s: %w", rawDiskPath, err)
			}

			files = append(files, File{
				ID:   fileRefID,
				Href: fileName,
				Size: diskCapacity,
			})

			// Create Disk section entry
			diskID := fmt.Sprintf("vmdisk%d", diskIndex)
			disks = append(disks, Disk{
				Capacity:                diskCapacity,
				CapacityAllocationUnits: "byte",
				DiskID:                  diskID,
				FileRef:                 fileRefID,
				Format:                  "http://www.vmware.com/interfaces/specifications/vmdk.html#streamOptimized",
			})

			hardwareItems = append(hardwareItems, Item{
				InstanceID:      strconv.Itoa(itemInstanceID),
				ResourceType:    17,
				ElementName:     fmt.Sprintf("Hard Disk %d", i+1),
				Description:     "Hard Disk",
				HostResource:    fmt.Sprintf("ovf:/disk/%s", diskID),
				Parent:          ideControllerID,
				AddressOnParent: strconv.Itoa(i),
			})
			itemInstanceID++
		}
	}

	// 4. Network Interfaces
	if adapters, ok := vmMap["NetworkAdapters"].([]interface{}); ok {
		for i, a := range adapters {

			adapter, ok := a.(map[string]interface{})
			if !ok {
				continue
			}

			networkIndex := i + 1
			networkName := fmt.Sprintf("VM Network %d", networkIndex)
			if n, ok := adapter["Name"].(string); ok && n != "" {
				networkName = n
			}

			networks = append(networks, Network{
				Name:        networkName,
				Description: fmt.Sprintf("Network interface %d", networkIndex),
			})

			autoAlloc := true
			hardwareItems = append(hardwareItems, Item{
				InstanceID:          strconv.Itoa(itemInstanceID),
				ResourceType:        10,
				ResourceSubType:     "E1000",
				ElementName:         fmt.Sprintf("Ethernet %d", networkIndex),
				Description:         fmt.Sprintf("E1000 ethernet adapter on \"%s\"", networkName),
				Connection:          networkName,
				AutomaticAllocation: &autoAlloc,
			})
			itemInstanceID++
		}
	}

	// --- Operating System ---
	vmName := "VM"
	if n, ok := vmMap["Name"].(string); ok {
		vmName = n
	}

	var guestOSInfo osutil.GuestOSInfo
	if guestMap, ok := vmMap["GuestOSInfo"].(map[string]interface{}); ok {
		if caption, ok := guestMap["Caption"].(string); ok {
			guestOSInfo.Caption = caption
		}
		if version, ok := guestMap["Version"].(string); ok {
			guestOSInfo.Version = version
		}
		if arch, ok := guestMap["OSArchitecture"].(string); ok {
			guestOSInfo.OSArchitecture = arch
		}
	}

	osType := osutil.MapCaptionToOsType(guestOSInfo.Caption, guestOSInfo.OSArchitecture)
	description := fmt.Sprintf("%s (%s)", guestOSInfo.Caption, guestOSInfo.OSArchitecture)

	env := &Envelope{
		Xmlns: "http://schemas.dmtf.org/ovf/envelope/1",
		Cim:   "http://schemas.dmtf.org/wbem/wscim/1/common",
		Ovf:   "http://schemas.dmtf.org/ovf/envelope/1",
		Rasd:  "http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData",
		Vmw:   "http://www.vmware.com/schema/ovf",
		Vssd:  "http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_VirtualSystemSettingData",
		Xsi:   "http://www.w3.org/2001/XMLSchema-instance",

		References: References{Files: files},
		DiskSection: DiskSection{
			Info:  "List of the virtual disks",
			Disks: disks,
		},
		NetworkSection: NetworkSection{
			Info:     "The list of logical networks",
			Networks: networks,
		},
		VirtualSystem: VirtualSystem{
			ID:   vmName,
			Info: "A Virtual system",
			Name: vmName,
			OperatingSystem: OperatingSystemSection{
				ID:          GetOVFOperatingSystemID(osType),
				OsType:      osType,
				Info:        "The operating system installed",
				Description: description,
			},
			VirtualHardware: VirtualHardwareSection{
				Info: "Virtual hardware requirements",
				System: System{
					ElementName:             "Virtual Hardware Family",
					InstanceID:              0,
					VirtualSystemIdentifier: vmName,
					VirtualSystemType:       "vmx-07",
				},
				Items: hardwareItems,
			},
		},
	}

	ovf, err := MarshalOvf(env)
	if err != nil {
		return fmt.Errorf("failed to marshal OVF: %w", err)
	}

	ovfPath := hyperv.RemoveFileExtension(remptePath) + ".ovf"
	os.WriteFile(ovfPath, ovf, 0644)
	fmt.Println("OVF file written to:", ovfPath)

	return nil
}

func MarshalOvf(env *Envelope) ([]byte, error) {
	body, err := xml.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, err
	}
	return []byte(xmlHeader + string(body)), nil
}
