# 🛠️ Hyper-V to OpenShift 

This tool automates the migration of VMs from **Hyper-V** to **OpenShift Virtualization**. It connects to Hyper-V , extracts VM metadata, converts `.vhdx` disks to `.raw`, generates OVF files, and applies the migration plan to OpenShift.

---

## 🚀 Features

- Connects to a remote **Hyper-V host** 
- Downloads VM `.vhdx` disks 
- Converts `.vhdx` to `.raw` using `virt-v2v`
- Generates an **OVF** descriptor for the VM
- Creates an **OVA Provider** in Forklift based on the OVF
- Applies a **migration plan** using OpenShift CRDs
- Executes the migration and monitors its status in real time

---

## 📋 Prerequisites

### ✅ System Requirements

- Go 1.20+
- Access to an OpenShift cluster with:
  - Forklift Operator installed
  - Access to a shared NFS storage

### 🗃️ NFS Server 

- To support OVA provider creation and migration processes, an accessible NFS server is required.

    - This server is used to:

        - Host the VHDX disk images and their associated OVF files.

        - Provide shared storage that the Forklift controller and OVA provider can access during conversion and transfer.
    

### ✅ Tools Required

`qemu-img` and  `virt-v2v` on local host : For VHDX to RAW conversion  

    sudo dnf install @virtualization qemu-kvm libvirt virt-install bridge-utils virt-manager -y
    sudo systemctl enable --now libvirtd
    sudo dnf install virt-v2v -y
    
    windows drivers :
        dnf install -y https://kojihub.stream.centos.org/kojifiles/packages/virtio-win/1.9.40/1.el9/noarch/virtio-win-1.9.40-1.el9.noarch.rpm



🧩 WinRM Setup on hyperV

    winrm quickconfig
    Set-Item -Path WSMan:\localhost\Service\Auth\Basic -Value $true
    Set-Item -Path WSMan:\localhost\Service\AllowUnencrypted -Value $true
    Restart-Service WinRM

🔐 SSH Setup on hyperV (Optional for disk download)

    Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
    Start-Service sshd
    Set-Service -Name sshd -StartupType 'Automatic'
    New-NetFirewallRule -Name sshd -DisplayName 'OpenSSH Server (sshd)' -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22

🔧 Environment Variables
    
    Set the following environment variables before running the tool:

    HYPERV_USER=
    HYPERV_PASS=
    HYPERV_HOST=
    HYPERV_PORT=5985
    SSH_PORT=22

    CLUSTER_NAME=
    MOUNT_BASH_PATH=
    CLUSTER_NFS_SERVER_PATH=
    OVA_PROVIDER_NFS_SERVER_PATH=
    NAMESPACE=

    You can export them into your shell or store in a .env file and load using source .env.



 ## 🛠️  Building and Running with Docker

This project can be built and run either natively on your system or inside a Docker container.

### Native Build

To build the tool directly on your system, run:

```sh
go build -o hyperv ./cmd/
```

### Docker Build

To build and run the tool in a container:

1. **Build the Docker image:**

   ```sh
   docker build -t hyperv-ova .
   ```

2. **Run the container:**

   ```sh
   docker run --rm -it \
        --privileged \
        -v $(pwd)/output:/output \
        --network host \
        -e HYPERV_USER=youruser \
        -e HYPERV_PASS=yourpass \
        -e HYPERV_HOST=yourhost \
        -e HYPERV_PORT=5985 \
        -e SSH_PORT=22 \
        -e NAMESPACE=your-namespace \
        -e OVA_PROVIDER_NFS_SERVER_PATH=your-nfs-path \
        hyperv-ova
   ```

    or (with .env file)
    ```sh
    docker run --rm -it \
        --privileged \
        --env-file .env \
        -v $(pwd)/output:/output \
        --network host \
        hyperv-ova
    ```

   - The `-v $(pwd)/output:/output` option mounts the output directory to persist files on your host.
   - Set all required environment variables as shown above.

### Notes

- The Docker image installs all required system dependencies, including Go, `qemu-img`, `virt-v2v`, and `openssh-clients`.
- You can use either the native build or the Docker container, depending on your environment and preference.