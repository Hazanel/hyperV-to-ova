FROM fedora:40

# Install system dependencies
RUN dnf -y install \
    wget \
    tar \
    qemu-img \
    virt-v2v \
    openssh-clients \
    git \
    unzip \
 && wget https://go.dev/dl/go1.24.4.linux-amd64.tar.gz \
 && rm -rf /usr/local/go \
 && tar -C /usr/local -xzf go1.24.4.linux-amd64.tar.gz \
 && ln -s /usr/local/go/bin/go /usr/bin/go \
 && dnf install -y https://kojihub.stream.centos.org/kojifiles/packages/virtio-win/1.9.40/1.el9/noarch/virtio-win-1.9.40-1.el9.noarch.rpm \
 && dnf clean all \
 && rm -f go1.24.4.linux-amd64.tar.gz


RUN wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable/openshift-client-linux.tar.gz \
 && tar -xzvf openshift-client-linux.tar.gz -C /usr/local/bin oc kubectl \
 && rm -f openshift-client-linux.tar.gz

# Set environment
ENV PATH="/usr/local/go/bin:${PATH}"
ENV LIBGUESTFS_BACKEND=direct

# Set up working directory
WORKDIR /app

# Copy Go modules first for better build caching
COPY go.mod go.sum ./

# Download Go dependencies
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build Go binary
RUN go build -o hyperv ./cmd/

# Expose SSH and WinRM ports if needed (optional)
EXPOSE 22 5985

# Set entrypoint
ENTRYPOINT ["./hyperv"]
