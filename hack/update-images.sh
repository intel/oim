#! /bin/sh

set -e

latest_image () (
    repo="$1"
    base="$2"

    # It would be better to check for the latest published image, but that seems to be hard
    # at the moment (https://stackoverflow.com/questions/28320134/how-to-list-all-tags-for-a-docker-image-on-a-remote-registry).
    version="$(git ls-remote "$repo" "refs/tags/v$base*" | sed -e 's;.*refs/tags/v;;' | grep -v -e '-pre'  -e '-rc' | sort -n | tail -1)"
    if [ ! "$version" ]; then
        echo "failed to find latest release for base version $base in $repo" >&2
        exit 1
    fi
    echo -n v$version
)

patch_image () (
    yaml="$1"
    image="$2"
    repo="$3"
    base="$4"

    sed -i -e "s;image:\(.*$image\):v.*;image:\1:$(latest_image $repo $base);" $yaml
)

patch_image deploy/kubernetes/malloc/malloc-daemonset.yaml csi-provisioner https://github.com/kubernetes-csi/external-provisioner.git 1.
patch_image deploy/kubernetes/malloc/malloc-daemonset.yaml csi-attacher https://github.com/kubernetes-csi/external-attacher.git 1.
patch_image deploy/kubernetes/malloc/malloc-daemonset.yaml csi-node-driver-registrar https://github.com/kubernetes-csi/node-driver-registrar.git 1.
