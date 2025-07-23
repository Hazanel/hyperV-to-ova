package nfs

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

type ProgressReader struct {
	Reader     io.Reader
	Total      int64
	ReadSoFar  int64
	lastUpdate time.Time
}

func PromptPassword() (string, error) {
	fmt.Print("Enter sudo password: ")
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return string(bytePassword), nil
}

func (pr *ProgressReader) printProgress() {
	percent := float64(pr.ReadSoFar) / float64(pr.Total) * 100
	fmt.Printf("\rCopying... %.2f%% (%d / %d bytes)", percent, pr.ReadSoFar, pr.Total)
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

func printProgress(done, total int64) {
	percent := float64(done) / float64(total) * 100
	fmt.Printf("\rCopying... %d/%d bytes (%.2f%%)", done, total, percent)
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

func CreateInOutput(fullPath string) (*os.File, error) {
	// Ensure the parent directory exists
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create parent directories: %w", err)
	}
	return os.Create(fullPath)
}

func CopyFile(srcPath, dstPath string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := CreateInOutput(dstPath)
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

func CopyToNFSServer(srcPath string) error {
	nfsServerPath := os.Getenv("OVA_PROVIDER_NFS_SERVER_PATH")
	if nfsServerPath == "" {
		return fmt.Errorf("NFS server path is required")
	}

	password, err := PromptPassword()
	if err != nil {
		return fmt.Errorf("password prompt failed: %w", err)
	}

	if err := RunCopyWithSudo(srcPath, nfsServerPath, password); err != nil {
		return fmt.Errorf("failed to copy files with sudo: %w", err)
	}

	return nil
}
