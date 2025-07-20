package osutil

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Map OS names (lowercase) to OVF OS IDs
var OsNameToID = map[string]int{
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

func ParseGuestOSInfo(data interface{}) (map[string]interface{}, error) {
	// Expecting the result to be either a map or a list of maps
	switch v := data.(type) {
	case map[string]interface{}:
		return v, nil
	case []interface{}:
		if len(v) > 0 {
			if first, ok := v[0].(map[string]interface{}); ok {
				return first, nil
			}
		}
		return nil, fmt.Errorf("unexpected list format: %+v", v)
	default:
		return nil, fmt.Errorf("unexpected type: %T", v)
	}
}

func MapCaptionToOsType(caption, arch string) string {
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

// Checks if a path is on a mounted filesystem (Linux only)
func isMounted(path string) (bool, error) {
	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}

	var statfs syscall.Statfs_t
	if err := syscall.Statfs(absPath, &statfs); err != nil {
		return false, err
	}

	// On Linux, Type 0x6969 is NFS, 0xEF53 is ext2/3/4, etc.
	// But here, we'll check if path exists and is accessible; if Statfs succeeds, it's mounted.
	// To be more precise, you might compare device IDs with /etc/mtab, but this is a simpler heuristic.
	return true, nil
}

type ProgressReader struct {
	Reader     io.Reader
	Total      int64
	ReadSoFar  int64
	lastUpdate time.Time
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	pr.ReadSoFar += int64(n)

	if time.Since(pr.lastUpdate) > 100*time.Millisecond {
		pr.printProgress()
		pr.lastUpdate = time.Now()
	}
	return n, err
}

func (pr *ProgressReader) printProgress() {
	percent := float64(pr.ReadSoFar) / float64(pr.Total) * 100
	fmt.Printf("\rCopying... %.2f%% (%d / %d bytes)", percent, pr.ReadSoFar, pr.Total)
}

func CopyFile(srcPath, dstPath string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	// Try sendfile on Linux
	if runtime.GOOS == "linux" {
		err := copyFileEfficient(srcFile, dstFile)
		if err == nil {
			fmt.Printf("Copied %s to %s using sendfile\n", srcPath, dstPath)
			return nil
		}
		fmt.Printf("sendfile failed, falling back to io.Copy: %v\n", err)
	}

	// Fall back to io.Copy with progress
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}
	totalSize := srcInfo.Size()

	progressReader := &ProgressReader{
		Reader: srcFile,
		Total:  totalSize,
	}

	_, err = io.Copy(dstFile, progressReader)
	fmt.Println() // newline after final progress
	if err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	fmt.Printf("Copied %s to %s\n", srcPath, dstPath)
	return nil
}

func copyFileEfficient(srcFile, dstFile *os.File) error {
	srcFd := int(srcFile.Fd())
	dstFd := int(dstFile.Fd())

	info, err := srcFile.Stat()
	if err != nil {
		return err
	}
	size := info.Size()

	var offset int64 = 0
	const chunkSize = 32 * 1024 * 1024 // 32MB

	start := time.Now()
	for offset < size {
		remain := size - offset
		chunk := int(remain)
		if chunk > chunkSize {
			chunk = chunkSize
		}

		n, err := unix.Sendfile(dstFd, srcFd, &offset, chunk)
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}

		offset += int64(n)
		printProgress(offset, size)
	}
	fmt.Print("\r") // clear progress line
	fmt.Printf("Copied using sendfile in %v\n", time.Since(start))
	return nil
}

func printProgress(done, total int64) {
	percent := float64(done) / float64(total) * 100
	fmt.Printf("\rCopying... %d/%d bytes (%.2f%%)", done, total, percent)
}

// CopyFilesInDir copies all .raw and .ovf files from output dir to nfs server
func CopyFilesNfsServer(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// skip inaccessible files/directories
			return nil
		}
		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".raw" && ext != ".ovf" {
			return nil
		}

		dstPath := filepath.Join(dstDir, d.Name())
		if err := CopyFile(path, dstPath); err != nil {
			return err
		}

		log.Printf("Copied %s to %s", path, dstPath)
		return nil
	})
}

// runCopyWithSudo runs the current program itself with sudo and a special flag
func RunCopyWithSudo(srcDir, dstDir, sudoPassword string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	fmt.Printf("Executing: sudo -S  %s --copy-files %s %s\n", self, srcDir, dstDir)

	cmd := exec.Command("sudo", "-S", self, "--copy-files", srcDir, dstDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	go func() {
		defer stdin.Close()
		io.WriteString(stdin, sudoPassword+"\n")
	}()

	return cmd.Run()
}

func PromptPassword() (string, error) {
	fmt.Print("Enter sudo password: ")
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // for newline after password input
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	return string(bytePassword), nil
}
