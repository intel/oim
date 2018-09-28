The .yaml files here demonstrate how to deploy OIM on Kubernetes such
that it provisions and uses SPDK Malloc BDevs. This only works when
all pods (OIM and app) run on the same node and that node is connected
to SPDK.
