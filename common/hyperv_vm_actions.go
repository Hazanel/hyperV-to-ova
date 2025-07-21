package common

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bramvdbogaerde/go-scp"
	"github.com/bramvdbogaerde/go-scp/auth"
	"github.com/joho/godotenv"
	"github.com/masterzen/winrm"
	"golang.org/x/crypto/ssh"
)

type VMAction string

const (
	ListVMs   VMAction = "list"
	GetVMInfo VMAction = "info"

	Shutdown VMAction = "Stop-VM -Name '%s' -Force -Confirm:$false"
	Start    VMAction = "Start-VM -Name '%s'"
	Save     VMAction = "Save-VM -Name '%s'"
	Pause    VMAction = "Suspend-VM -Name '%s'"
	Resume   VMAction = "Resume-VM -Name '%s'"
	Remove   VMAction = "Remove-VM -Name '%s' -Force -Confirm:$false"
	Restart  VMAction = "Restart-VM -Name '%s' -Force -Confirm:$false"
)

type PSOptions struct {
	ParseJSON bool
	Depth     int
	Compress  bool
	AsJSON    bool
}

func runPSCommand(client *winrm.Client, baseCommand string, opts PSOptions) (interface{}, error) {
	var psCommand string

	if opts.AsJSON {
		depth := 2
		if opts.Depth > 0 {
			depth = opts.Depth
		}
		compressFlag := ""
		if opts.Compress {
			compressFlag = " -Compress"
		}
		psCommand = fmt.Sprintf(`powershell -Command "%s | ConvertTo-Json -Depth %d%s"`, baseCommand, depth, compressFlag)
	} else {
		psCommand = fmt.Sprintf(`powershell -Command "%s"`, baseCommand)
	}

	var stdout, stderr bytes.Buffer
	exitCode, err := client.Run(psCommand, &stdout, &stderr)
	if err != nil {
		return nil, fmt.Errorf("command failed: %w\nSTDERR: %s\nSTDOUT: %s", err, stderr.String(), stdout.String())
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("command exited with code %d\nSTDERR: %s\nSTDOUT: %s", exitCode, stderr.String(), stdout.String())
	}

	if opts.ParseJSON {
		var result interface{}
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w\nRaw Output:\n%s", err, stdout.String())
		}
		return result, nil
	}

	return stdout.String(), nil
}

func performVMAction(client *winrm.Client, vmName string, action VMAction) error {
	fmt.Printf("Executing VM action: %s on '%s'\n", strings.Fields(string(action))[0], vmName)

	cmd := fmt.Sprintf(string(action), vmName)
	_, err := runPSCommand(client, cmd, PSOptions{})
	if err != nil {
		return fmt.Errorf("VM action failed (%s): %w", action, err)
	}

	fmt.Printf("Action %s completed successfully on '%s'\n", strings.Fields(string(action))[0], vmName)
	return nil
}

func GetGuestOSInfoFromVM(client *winrm.Client, vmName, guestUser, guestPassword string) (interface{}, error) {
	psCmd := fmt.Sprintf(`$secpasswd = ConvertTo-SecureString '%s' -AsPlainText -Force; `+
		`$cred = New-Object System.Management.Automation.PSCredential('%s', $secpasswd); `+
		`Invoke-Command -VMName '%s' -Credential $cred -ScriptBlock { `+
		`Get-CimInstance Win32_OperatingSystem | Select Caption, Version, OSArchitecture }`,
		guestPassword, guestUser, vmName)

	return runPSCommand(client, psCmd, PSOptions{
		AsJSON:    true,
		Compress:  true,
		ParseJSON: true,
	})
}

func getVMInfo(client *winrm.Client, vmName string) (interface{}, error) {
	return runPSCommand(client, fmt.Sprintf("Get-VM -Name '%s'", vmName), PSOptions{
		AsJSON:    true,
		ParseJSON: true,
		Depth:     3,
	})
}

func getVMNames(client *winrm.Client) ([]string, error) {
	out, err := runPSCommand(client, "Get-VM | Select -ExpandProperty Name", PSOptions{})
	if err != nil {
		return nil, err
	}
	outputStr := out.(string)
	names := strings.Split(strings.TrimSpace(outputStr), "\n")
	for i := range names {
		names[i] = strings.TrimSpace(names[i])
	}
	return names, nil
}

func PerformVMAction(client *winrm.Client, vmName string, action VMAction) (interface{}, error) {
	switch action {
	case ListVMs:
		return getVMNames(client)
	case GetVMInfo:
		return getVMInfo(client, vmName)
	case Shutdown, Start, Save, Pause, Resume, Remove, Restart:
		err := performVMAction(client, vmName, action)
		return nil, err
	default:
		return nil, fmt.Errorf("unsupported VM action: %s", action)
	}
}

// CopyRemoteFileWithProgress connects via SSH, copies a file from the remote host, and shows progress.
func CopyRemoteFileWithProgress(user, password, host, sshPort, remotePath, localFilename string) error {
	clientConfig, err := auth.PasswordKey(user, password, ssh.InsecureIgnoreHostKey())
	if err != nil {
		return fmt.Errorf("failed to create SSH client config: %w", err)
	}

	// Build SSH address
	sshAddr := fmt.Sprintf("%s:%s", host, sshPort)

	scpClient := scp.NewClient(sshAddr, &clientConfig)
	if err := scpClient.Connect(); err != nil {
		return fmt.Errorf("failed to connect to SSH: %w", err)
	}
	defer scpClient.Close()

	file, err := os.Create(localFilename)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer file.Close()

	done := make(chan struct{})
	go showProgress(file.Name(), done)

	err = scpClient.CopyFromRemote(context.Background(), file, remotePath)
	close(done) // stop the progress ticker

	if err != nil {
		return fmt.Errorf("failed to copy from remote: %w", err)
	}

	fmt.Println("\n Download complete.")
	return nil
}

func showProgress(filename string, done <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			fmt.Print("\r") // clear line on exit
			return
		case <-ticker.C:
			fi, err := os.Stat(filename)
			if err == nil {
				fmt.Printf("\rDownloading... %d bytes      ", fi.Size())
			}
		}
	}
}

// LoadHyperVConnection loads environment variables and returns:
// - a WinRM client
// - host IP
// - SSH host string (ip:port)
// - user and password
func LoadHyperVConnection() (*winrm.Client, string, string, string, string, error) {
	if err := godotenv.Load(); err != nil {
		return nil, "", "", "", "", fmt.Errorf("error loading .env file: %w", err)
	}

	// Load and validate required environment variables
	user := os.Getenv("HYPERV_USER")
	password := os.Getenv("HYPERV_PASS")
	hostIP := os.Getenv("HYPERV_HOST")

	if user == "" || password == "" || hostIP == "" {
		return nil, "", "", "", "", fmt.Errorf("missing credentials in environment (HYPERV_USER/HYPERV_PASS/HYPERV_HOST)")
	}

	winrmPortStr := os.Getenv("HYPERV_PORT")
	if winrmPortStr == "" {
		return nil, "", "", "", "", fmt.Errorf("missing HYPERV_PORT in environment")
	}

	winrmPort, err := strconv.Atoi(winrmPortStr)
	if err != nil {
		return nil, "", "", "", "", fmt.Errorf("invalid HYPERV_PORT: %v", err)
	}

	sshPort := os.Getenv("SSH_PORT")
	if sshPort == "" {
		sshPort = "22"
	}

	endpoint := winrm.NewEndpoint(hostIP, winrmPort, false, false, nil, nil, nil, 0)
	client, err := winrm.NewClient(endpoint, user, password)
	if err != nil {
		return nil, "", "", "", "", fmt.Errorf("failed to create WinRM client: %v", err)
	}

	fmt.Printf("Connected to Hyper-V at %s (SSH) and WinRM port %d\n", hostIP, winrmPort)
	return client, hostIP, sshPort, user, password, nil
}
