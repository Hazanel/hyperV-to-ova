package ova

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Map OS names (lowercase) to OVF OS IDs
var osNameToID = map[string]int{
	"otherGuest":             1,
	"macosGuest":             2,
	"attunixGuest":           3,
	"dguxGuest":              4,
	"windowsxpGuest":         5,
	"windows2000Guest":       6,
	"windows2003Guest":       7,
	"vistaGuest":             8,
	"windows7Guest":          9,
	"windows8Guest":          10,
	"windows81Guest":         11,
	"windows10Guest":         103,
	"windows10_64Guest":      103,
	"windows11Guest":         103,
	"windows11_64Guest":      103,
	"windows7srv_guest":      13,
	"windows7srv_64Guest":    13,
	"windows8srv_guest":      112,
	"windows8srv_64Guest":    112,
	"windows2016srv_guest":   15,
	"windows2016srv_64Guest": 15,
	"windows2019srv_guest":   94,
	"windows2019srv_64Guest": 94,
	"windows2022srv_guest":   17,
	"windows2022srv_64Guest": 17,
	"rhel7Guest":             20,
	"rhel8_64Guest":          21,
	"ubuntuGuest":            22,
	"ubuntu64Guest":          22,
	"centosGuest":            24,
	"centos64Guest":          24,
	"debian10Guest":          26,
	"debian10_64Guest":       26,
	"fedoraGuest":            27,
	"fedora64Guest":          27,
	"slesGuest":              29,
	"sles_64Guest":           30,
	"solaris10Guest":         31,
	"solaris11Guest":         32,
	"freebsd11Guest":         33,
	"freebsd12Guest":         34,
	"oracleLinuxGuest":       35,
	"oracleLinux64Guest":     36,
	"otherLinuxGuest":        101,
	"otherLinux64Guest":      101,
}

type GuestOSInfo struct {
	Caption        string `json:"Caption"`
	Version        string `json:"Version"`
	OSArchitecture string `json:"OSArchitecture"`
}

// GetOVFOperatingSystemID returns the OVF OS ID for a given OS name string.
func GetOVFOperatingSystemID(osName string) int {
	// Normalize input to lowercase for exact match
	key := strings.ToLower(osName)

	// Exact match
	if id, found := osNameToID[key]; found {
		return id
	}

	// Fallback: partial match
	for known, id := range osNameToID {
		if strings.Contains(key, strings.ToLower(known)) {
			return id
		}
	}

	// Final fallback
	return 1 // Other
}

func FormatFromHyperV(vm interface{}) ([]byte, error) {

	vmMap, ok := vm.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid VM format: expected map[string]interface{}")
	}

	var (
		files          []File
		disks          []Disk
		networks       []Network
		hardwareItems  []Item
		itemInstanceID = 1
	)

	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot get working directory: %w", err)
	}

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
		for i, raw := range hdList {
			hd, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}

			diskIndex := i + 1
			fileRefID := fmt.Sprintf("file%d", diskIndex)
			pathRaw, _ := hd["Path"].(string)
			// Normalize Windows-style path to POSIX-style for path.Base to work
			normalized := strings.ReplaceAll(pathRaw, "\\", "/")
			base := filepath.Base(normalized)                        // e.g., "disk1.vhdx"
			baseName := strings.TrimSuffix(base, filepath.Ext(base)) // e.g., "disk1"
			fileName := baseName + ".raw"
			rawDiskPath := filepath.Join(wd, fileName)

			diskCapacity := int64(10 * 1024 * 1024 * 1024) // fallback size
			if stat, err := os.Stat(rawDiskPath); err == nil {
				diskCapacity = stat.Size()
			} else {
				return nil, fmt.Errorf("failed to get size of raw disk file %s: %w", rawDiskPath, err)
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

	var guestOSInfo GuestOSInfo
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

	osType := mapCaptionToOsType(guestOSInfo.Caption, guestOSInfo.OSArchitecture)
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

	return MarshalOvf(env)
}

func MarshalOvf(env *Envelope) ([]byte, error) {
	body, err := xml.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, err
	}
	return []byte(xmlHeader + string(body)), nil
}

func ParseGuestOSInfo(jsonStr string) (map[string]interface{}, error) {
	var info GuestOSInfo
	err := json.Unmarshal([]byte(jsonStr), &info)
	if err != nil {
		return nil, err
	}

	guestOSMap := map[string]interface{}{
		"Caption":        info.Caption,
		"Version":        info.Version,
		"OSArchitecture": info.OSArchitecture,
	}

	return guestOSMap, nil
}

func mapCaptionToOsType(caption, arch string) string {
	caption = strings.ToLower(caption)
	arch = strings.ToLower(arch)

	switch {
	// === Windows Server ===
	case strings.Contains(caption, "windows server 2022"):
		if arch == "64-bit" {
			return "windows2022srv_64Guest"
		}
		return "windows2022srv_guest"
	case strings.Contains(caption, "windows server 2019"):
		if arch == "64-bit" {
			return "windows2019srv_64Guest"
		}
		return "windows2019srv_guest"
	case strings.Contains(caption, "windows server 2016"):
		if arch == "64-bit" {
			return "windows2016srv_64Guest"
		}
		return "windows2016srv_guest"
	case strings.Contains(caption, "windows server 2012 r2"):
		if arch == "64-bit" {
			return "windows8srv_64Guest"
		}
		return "windows8srv_guest"
	case strings.Contains(caption, "windows server 2012"):
		if arch == "64-bit" {
			return "windows8srv_64Guest"
		}
		return "windows8srv_guest"
	case strings.Contains(caption, "windows server 2008 r2"):
		if arch == "64-bit" {
			return "windows7srv_64Guest"
		}
		return "windows7srv_guest"

	// === Windows Desktop ===
	case strings.Contains(caption, "windows 11"):
		if arch == "64-bit" {
			return "windows11_64Guest"
		}
		return "windows11Guest"
	case strings.Contains(caption, "windows 10"):
		if arch == "64-bit" {
			return "windows10_64Guest"
		}
		return "windows10Guest"
	case strings.Contains(caption, "windows 8.1"):
		if arch == "64-bit" {
			return "windows8_64Guest"
		}
		return "windows8Guest"
	case strings.Contains(caption, "windows 8"):
		if arch == "64-bit" {
			return "windows8_64Guest"
		}
		return "windows8Guest"
	case strings.Contains(caption, "windows 7"):
		if arch == "64-bit" {
			return "windows7_64Guest"
		}
		return "windows7Guest"
	case strings.Contains(caption, "windows vista"):
		if arch == "64-bit" {
			return "vista_64Guest"
		}
		return "vistaGuest"

	// === Linux ===
	case strings.Contains(caption, "ubuntu"):
		if arch == "64-bit" {
			return "ubuntu64Guest"
		}
		return "ubuntuGuest"
	case strings.Contains(caption, "debian"):
		if arch == "64-bit" {
			return "debian10_64Guest"
		}
		return "debian10Guest"
	case strings.Contains(caption, "centos"):
		if arch == "64-bit" {
			return "centos64Guest"
		}
		return "centosGuest"
	case strings.Contains(caption, "red hat enterprise linux") || strings.Contains(caption, "rhel"):
		if arch == "64-bit" {
			return "rhel8_64Guest"
		}
		return "rhel7Guest"
	case strings.Contains(caption, "suse"):
		if arch == "64-bit" {
			return "sles_64Guest"
		}
		return "slesGuest"
	case strings.Contains(caption, "fedora"):
		if arch == "64-bit" {
			return "fedora64Guest"
		}
		return "fedoraGuest"
	case strings.Contains(caption, "oracle linux"):
		if arch == "64-bit" {
			return "oracleLinux64Guest"
		}
		return "oracleLinuxGuest"
	case strings.Contains(caption, "linux"):
		if arch == "64-bit" {
			return "otherLinux64Guest"
		}
		return "otherLinuxGuest"

	default:
		return "otherGuest"
	}
}
