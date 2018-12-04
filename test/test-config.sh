# This file is meant to be sourced into various scripts in this directory and provides
# some common settings.

# The container runtime that is meant to be used inside Clear Linux.
# Possible values are "docker" and "crio".
#
# Docker is the default because:
# - survives killing the VMs while cri-o doesn't (https://github.com/kubernetes-sigs/cri-o/issues/1742#issuecomment-442384980)
TEST_CRI=docker

# Prefix for network devices etc.
TEST_PREFIX=oim

# IPv4 base address.
TEST_IP_ADDR=192.168.7

# IP addresses of DNS servers to use inside the VMs, separated by spaces.
# The default is to use the ones specified in /etc/resolv.conf, but those
# might not be reachable from inside the VMs (like for example, 127.0.0.53
# from systemd-network).
TEST_DNS_SERVERS=

# Additional Clear Linux bundles.
# storage-utils is needed because of https://github.com/clearlinux/distribution/issues/217
TEST_CLEAR_LINUX_BUNDLES="storage-cluster storage-utils"

# Post-install command for each virtual machine. Called with the
# current image number (0 to n-1) as parameter.
TEST_CLEAR_LINUX_POST_INSTALL=do_enable_lvm

# Called after Kubernetes has been configured and started on the master node.
TEST_CONFIGURE_POST_MASTER=do_configure_post_master

# Called after Kubernetes has been configured and started.
TEST_CONFIGURE_POST_ALL=do_configure_post_all

# allow overriding the configuration in additional file(s)
if [ -d test/test-config.d ]; then
    for i in $(ls test/test-config.d/*.sh 2>/dev/null | sort); do
        . $i
    done
fi

do_enable_lvm () {
    imagenum=$1
    # Ceph needs LVM.
    _work/ssh-clear-kvm.$imagenum 'systemctl enable lvm2-lvmetad.socket && systemctl start lvm2-lvmetad.socket'
}

do_configure_post_master () {
    # Allow normal pods on master node. This is particularly important because
    # we only attach SPDK to that node, so pods using storage acceleration *have*
    # to run on that node. To ensure that, we set the "intel.com/oim" label to 1 and
    # use that as a node selector.
    _work/ssh-clear-kvm.0 kubectl taint nodes --all node-role.kubernetes.io/master-
    _work/ssh-clear-kvm.0 kubectl label nodes host-0 intel.com/oim=1
}

do_configure_post_all () {
    # Configure ceph.
    # Based on http://docs.ceph.com/docs/master/install/manual-deployment/
    _work/ssh-clear-kvm.0 mkdir /etc/ceph
    fsid=$(uuidgen)
    _work/ssh-clear-kvm.0 dd of=/etc/ceph/ceph.conf <<EOF
[global]
fsid = $fsid
mon initial members = host-0
mon host = 192.168.7.2
public network = 192.168.7.0/24
[mon]
# Clear Linux has less than the default 30% free after installation.
# We lower the threshold to avoid a health warning.
mon data avail warn = 5
EOF

# Create systemd units for ceph-mgr (https://github.com/clearlinux/distribution/issues/218).
    for i in $(seq 0 $LAST_NODE); do
        _work/ssh-clear-kvm.$i mkdir -p /etc/systemd/system
        _work/ssh-clear-kvm.$i dd of=/etc/systemd/system/ceph-mgr.target <<EOF
[Unit]
Description=ceph target allowing to start/stop all ceph-mgr@.service instances at once
PartOf=ceph.target
Before=ceph.target
[Install]
WantedBy=multi-user.target ceph.target
EOF
        _work/ssh-clear-kvm.$i dd of=/etc/systemd/system/ceph-mgr@.service <<EOF
[Unit]
Description=Ceph cluster manager daemon
After=network-online.target local-fs.target time-sync.target
Wants=network-online.target local-fs.target time-sync.target
PartOf=ceph-mgr.target

[Service]
LimitNOFILE=1048576
LimitNPROC=1048576
EnvironmentFile=-/etc/default/ceph
Environment=CLUSTER=ceph

ExecStart=/usr/bin/ceph-mgr -f --cluster \${CLUSTER} --id %i --setuser ceph --setgroup ceph
ExecReload=/bin/kill -HUP \$MAINPID
Restart=on-failure
StartLimitInterval=30min
StartLimitBurst=3

[Install]
WantedBy=ceph-mgr.target
EOF
    done

    # We have to feed a script into the remote shell here because 'allow
    # *' doesn't transmit well as a ssh parameter.
    _work/ssh-clear-kvm.0 <<EOF
set -ex
ceph-authtool --create-keyring /tmp/ceph.mon.keyring --gen-key -n mon. --cap mon 'allow *'
ceph-authtool --create-keyring /etc/ceph/ceph.client.admin.keyring --gen-key -n client.admin --cap mon 'allow *' --cap osd 'allow *' --cap mds 'allow *' --cap mgr 'allow *'
mkdir -p /var/lib/ceph/bootstrap-osd
ceph-authtool --create-keyring /var/lib/ceph/bootstrap-osd/ceph.keyring --gen-key -n client.bootstrap-osd --cap mon 'profile bootstrap-osd'
ceph-authtool /tmp/ceph.mon.keyring --import-keyring /etc/ceph/ceph.client.admin.keyring
ceph-authtool /tmp/ceph.mon.keyring --import-keyring /var/lib/ceph/bootstrap-osd/ceph.keyring
monmaptool --create --add host-0  192.168.7.2 --fsid $fsid /tmp/monmap
mkdir -p /var/lib/ceph/mon/ceph-host-0
chown ceph /var/lib/ceph/mon/ceph-host-0
chmod a+r /tmp/ceph.mon.keyring
sudo -u ceph ceph-mon --mkfs -i host-0 --monmap /tmp/monmap --keyring /tmp/ceph.mon.keyring
systemctl enable ceph-mon@host-0
systemctl start ceph-mon@host-0
systemctl enable ceph-mon.target

# Enable mgr (http://docs.ceph.com/docs/master/mgr/administrator/#mgr-administrator-guide)
mkdir -p /var/lib/ceph/mgr/ceph-host-0/
ceph auth get-or-create mgr.host-0 mon 'allow profile mgr' osd 'allow *' mds 'allow *' >/var/lib/ceph/mgr/ceph-host-0/keyring
ceph auth get-or-create client.kubernetes mon 'profile rbd' osd 'profile rbd pool=rbd' >/etc/ceph/ceph.client.kubernetes.keyring
systemctl enable ceph-mgr@host-0
systemctl start ceph-mgr@host-0
systemctl enable ceph-mgr.target

ceph -s
EOF

    # Copy Ceph config from master node to slaves.
    for i in $(seq 1 $LAST_NODE); do
        _work/ssh-clear-kvm.$i mkdir /etc/ceph
        _work/ssh-clear-kvm.0 cat /etc/ceph/ceph.conf | _work/ssh-clear-kvm.$i dd of=/etc/ceph/ceph.conf
        _work/ssh-clear-kvm.0 cat /etc/ceph/ceph.client.admin.keyring | _work/ssh-clear-kvm.$i dd of=/etc/ceph/ceph.client.admin.keyring
        _work/ssh-clear-kvm.$i mkdir -p /var/lib/ceph/bootstrap-osd/
        _work/ssh-clear-kvm.0 cat /var/lib/ceph/bootstrap-osd/ceph.keyring | _work/ssh-clear-kvm.$i dd of=/var/lib/ceph/bootstrap-osd/ceph.keyring
    done

    # Adding OSDs
    # http://docs.ceph.com/docs/master/install/manual-deployment/#short-form
    for i in $(seq 0 $LAST_NODE); do
        _work/ssh-clear-kvm.0 'cat >>/etc/ceph/ceph.conf' <<EOF
[osd.$i]
public addr = 192.168.7.$(($i * 2 + 2))
EOF
        _work/ssh-clear-kvm.$i ceph-volume lvm create --data /dev/vdb
        _work/ssh-clear-kvm.$i systemctl enable ceph-osd.target
    done
    _work/ssh-clear-kvm.0 ceph status


    # Create RBD pool called "rbd" (the default).
    # http://docs.ceph.com/docs/mimic/rados/operations/pools/#create-a-pool
    _work/ssh-clear-kvm.0 ceph osd pool create rbd 128 128
    _work/ssh-clear-kvm.0 rbd pool init rbd

    # Create secret for ceph-csi (https://github.com/ceph/ceph-csi/blob/master/examples/rbd/secret.yaml).
    _work/ssh-clear-kvm kubectl create -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: csi-rbd-secret
  namespace: default
data:
  admin: $(_work/ssh-clear-kvm grep key /etc/ceph/ceph.client.admin.keyring | sed -e 's/.* //' | base64 -w 0)
  kubernetes: $(_work/ssh-clear-kvm grep key /etc/ceph/ceph.client.kubernetes.keyring | sed -e 's/.* //' | base64 -w 0)
  monitors: $(echo 192.168.7.2:6789 | tr -d "\n" | base64 -w 0)
EOF
}
