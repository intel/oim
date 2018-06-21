FROM alpine
LABEL maintainers="Intel"
LABEL description="Open Infrastructure Manager CSI Driver"

COPY ./oim-csi-driver /oim-csi-driver
# Formatting a block volume depends on additional tools.
# blkid is used by k8s.io/kubernetes/pkg/util/mount/mount_linux.go.
RUN apk add --no-cache e2fsprogs xfsprogs btrfs-progs dosfstools blkid
ENTRYPOINT ["/oim-csi-driver"]
