package common

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ExtractPath(vm map[string]interface{}) (string, bool) {
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

func SaveVMJsonToFile(jsonOut []byte, filename string) error {

	err := os.WriteFile(filename, jsonOut, 0644)
	if err != nil {
		return fmt.Errorf("failed to write JSON to file: %w", err)
	}
	fmt.Printf("JSON output saved to: %s\n", filename)
	return nil
}

func RemoveFileExtension(filename string) string {
	ext := filepath.Ext(filename)
	return strings.TrimSuffix(filename, ext)
}

func AskYesNo(prompt string) bool {
	fmt.Print(prompt + " [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}
