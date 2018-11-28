# Brings up the emulator environment:
# - starts the OIM control plane and SPDK on the local host
# - creates an SPDK virtio-scsi controller
# - starts a QEMU virtual machine connected to SPDK's virtio-scsi controller
# - starts a Kubernetes cluster
# - deploys the OIM driver
start: _work/clear-kvm.img _work/kube-clear-kvm _work/start-clear-kvm _work/ssh-clear-kvm _work/ca/.ca-stamp _work/vhost
	if ! [ -e _work/oim-registry.pid ] || ! kill -0 $$(cat _work/oim-registry.pid) 2>/dev/null; then \
		truncate -s 0 _work/oim-registry.log && \
		( _output/oim-registry -endpoint tcp://192.168.7.1:0 -ca _work/ca/ca.crt -key _work/ca/component.registry.key -log.level DEBUG >>_work/oim-registry.log 2>&1 & echo $$! >_work/oim-registry.pid ) && \
		while ! grep -m 1 'listening for connections' _work/oim-registry.log > _work/oim-registry.port; do sleep 1; done && \
		sed -i -e 's/.*address: .*://' _work/oim-registry.port; \
	fi
	if ! [ -e _work/vhost.pid ] || ! sudo kill -0 $$(cat _work/vhost.pid) 2>/dev/null; then \
		rm -rf _work/vhost-run _work/vhost.pid && \
		mkdir _work/vhost-run && \
		( sudo _work/vhost -R -S $$(pwd)/_work/vhost-run -r $$(pwd)/_work/vhost-run/spdk.sock -f _work/vhost.pid -s 256 >_work/vhost.log 2>&1 & while ! [ -s _work/vhost.pid ] || ! [ -e _work/vhost-run/spdk.sock ]; do sleep 1; done ) && \
		sudo chmod a+rw _work/vhost-run/spdk.sock; \
	fi
	if ! [ -e _work/vhost-run/scsi0 ]; then \
		vendor/github.com/spdk/spdk/scripts/rpc.py -s $$(pwd)/_work/vhost-run/spdk.sock construct_vhost_scsi_controller scsi0 && \
		while ! [ -e _work/vhost-run/scsi0 ]; do sleep 1; done && \
		sudo chmod a+rw _work/vhost-run/scsi0; \
	fi
	if ! [ -e _work/oim-controller.pid ] || ! kill -0 $$(cat _work/oim-controller.pid) 2>/dev/null; then \
		( _output/oim-controller -endpoint unix://$$(pwd)/_work/oim-controller.sock \
		                         -ca _work/ca/ca.crt \
		                         -key _work/ca/controller.host-0.key \
		                         -spdk _work/vhost-run/spdk.sock \
		                         -vhost-scsi-controller scsi0 \
		                         -vm-vhost-device :.0 \
		                         -log.level DEBUG \
		                         >_work/oim-controller.log 2>&1 & echo $$! >_work/oim-controller.pid ) && \
		while ! grep -q 'listening for connections' _work/oim-controller.log; do sleep 1; done; \
	fi
	_output/oimctl -registry 192.168.7.1:$$(cat _work/oim-registry.port) -ca _work/ca/ca.crt -key _work/ca/user.admin.key \
		-set -path "host-0/address" -value "unix://$$(pwd)/_work/oim-controller.sock"
	_output/oimctl -registry 192.168.7.1:$$(cat _work/oim-registry.port) -ca _work/ca/ca.crt -key _work/ca/user.admin.key \
		-set -path "host-0/pci" -value "00:15."
	for i in $$(seq 0 $$(($(NUM_NODES) - 1))); do \
		if ! [ -e _work/clear-kvm.$$i.pid ] || ! kill -0 $$(cat _work/clear-kvm.$$i.pid) 2>/dev/null; then \
			if [ $$i -eq 0 ]; then \
				opts="-m 2048 -object memory-backend-file,id=mem,size=2048M,mem-path=/dev/hugepages,share=on -numa node,memdev=mem -chardev socket,id=vhost0,path=_work/vhost-run/scsi0 -device vhost-user-scsi-pci,id=scsi0,chardev=vhost0,bus=pci.0,addr=0x15"; \
			else \
				opts=; \
			fi; \
			_work/start-clear-kvm _work/clear-kvm.$$i.img -monitor none -serial file:/nvme/gopath/src/github.com/intel/oim/_work/clear-kvm.$$i.log $$opts & \
			echo $$! >_work/clear-kvm.$$i.pid; \
		fi; \
	done
	while ! _work/ssh-clear-kvm true 2>/dev/null; do \
		sleep 1; \
	done
	_work/kube-clear-kvm
	cat _work/ca/secret.yaml | _work/ssh-clear-kvm kubectl create -f -
	for i in malloc-rbac.yaml malloc-storageclass.yaml malloc-daemonset.yaml; do \
		cat deploy/kubernetes/malloc/$$i | \
			sed -e "s;@OIM_REGISTRY_ADDRESS@;192.168.7.1:$$(cat _work/oim-registry.port);" | \
			_work/ssh-clear-kvm kubectl create -f - || true; \
	done
	@ echo "The test cluster is ready. Log in with _work/ssh-clear-kvm, run kubectl once logged in."
	@ echo "To try out the OIM CSI driver:"
	@ echo "   cat deploy/kubernetes/malloc/example/malloc-pvc.yaml | _work/ssh-clear-kvm kubectl create -f -"
	@ echo "   cat deploy/kubernetes/malloc/example/malloc-app.yaml | _work/ssh-clear-kvm kubectl create -f -"

stop:
	if [ -e _work/clear-kvm.0.pid ]; then \
		cat _work/ca/secret.yaml | _work/ssh-clear-kvm kubectl delete -f - || true; \
		for i in example/malloc-app.yaml example/malloc-pvc.yaml malloc-rbac.yaml malloc-storageclass.yaml malloc-daemonset.yaml; do \
			cat deploy/kubernetes/malloc/$$i | _work/ssh-clear-kvm kubectl delete -f - || true; \
		done; \
	fi
	for i in $$(seq 0 $$(($(NUM_NODES) - 1))); do \
		if [ -e _work/clear-kvm.$$i.pid ]; then \
			kill -9 $$(cat _work/clear-kvm.$$i.pid) 2>/dev/null; \
			rm -f _work/clear-kvm.$$i.pid; \
		fi; \
	done
	if [ -e _work/oim-registry.pid ]; then \
		kill -9 $$(cat _work/oim-registry.pid) 2>/dev/null; \
		rm -f _work/oim-registry.pid; \
	fi
	if [ -e _work/oim-controller.pid ]; then \
		kill -9 $$(cat _work/oim-controller.pid) 2>/dev/null; \
		rm -f _work/oim-controller.pid; \
	fi
	if [ -e _work/vhost.pid ]; then \
		if grep -q vhost /proc/$$(cat _work/vhost.pid)/cmdline; then \
			sudo kill $$(cat _work/vhost.pid) 2>/dev/null; \
		fi; \
		rm -f _work/vhost.pid; \
	fi
