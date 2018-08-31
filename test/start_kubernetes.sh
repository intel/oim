#!/bin/sh -e

SSH 'systemctl start docker && systemctl start kubelet'
cnt=0
while [ $cnt -lt 60 ]; do
    if SSH kubectl get nodes >/dev/null 2>/dev/null; then
        exit 0
    fi
    cnt=$(expr $cnt + 1)
    sleep 1
done
echo "timed out waiting for Kubernetes"
exit 1
