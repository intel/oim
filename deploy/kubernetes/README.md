The .yaml deployment files in this directory have been updated to work
with Kubernetes 1.12 by enabling registration via
[CSI driver discovery](https://kubernetes-csi.github.io/docs/Setup.html#csi-driver-discovery-beta).
This makes them incompatible with older Kubernetes releases. The OIM
CSI driver itself still works with those.
