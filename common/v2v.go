package common

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// RemoveFileExtension strips the file extension from a filename.

// ConvertVHDXToRaw converts a VHDX file to RAW format using virt-v2v.
func ConvertVHDXToRaw(vhdxPath string) error {

	if _, err := exec.LookPath("virt-v2v"); err != nil {
		return fmt.Errorf("virt-v2v not found in PATH; please install it first")
	}

	fmt.Println("Converting to RAW format with virt-v2v...")

	cmd := exec.Command("virt-v2v", "-i", "disk", vhdxPath, "-o", "local", "-of", "raw", "-os", filepath.Dir(vhdxPath))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	rawFile := RemoveFileExtension(vhdxPath) + ".raw"
	convertedFile := vhdxPath + "-sda"

	if err := os.Rename(convertedFile, rawFile); err != nil {
		return fmt.Errorf("failed to rename converted file: %w", err)
	}

	fmt.Println("Conversion complete:", rawFile)
	return nil
}
