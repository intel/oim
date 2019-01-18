#! /bin/sh
#
# OIM reuses the upstream RBAC rules with modifications that are applied
# by this script:
# - all items with a custom name prefix
# - filter out unneeded items

set -e

WORKDIR=$(mktemp -d)
trap "rm -rf $WORKDIR" EXIT

patch_file () {
    url="$1"
    prefix=$2

    rm -f $WORKDIR/*
    file=$(basename "$url")
    echo "Patching $url" >&2
    wget -O $WORKDIR/$file "$url"

    # Split up into items at the "---" separator.
    items=0
    while IFS='' read -r line; do
        if [ "$line" = "---" ]; then
            items=$(($items + 1))
        else
            echo "$line" >>$WORKDIR/item.$items
        fi
    done <$WORKDIR/$file

    ls $WORKDIR >&2

    out=$WORKDIR/modified.yaml
    cat >$out <<EOF
# Based on $url.
# Modified by hack/update-rbac.sh.
# Do not edit manually!

EOF
    first=1
    for i in $(seq 0 $items); do
        # Role bindings and service account are in our own RBAC file.
        if grep -q -e '^kind: *\(RoleBinding\|ClusterRoleBinding\|ServiceAccount\)' $WORKDIR/item.$i; then
            continue
        fi

        # We don't enable leadership election.
        if grep -q -e 'if.*leadership election is enabled' $WORKDIR/item.$i; then
            continue
        fi

        if [ ! "$first" ]; then
            echo "---" >>$out
        fi

        # Rename.
        sed -e "s/name: /name: $prefix-/" $WORKDIR/item.$i >>$out

        first=
    done

    diff -c $WORKDIR/$file $out >&2 || true
    cat $out
}

image_version () {
    file="$1"
    image="$2"

    version=$(grep "image:.*$image" "$file" | sed -e 's/.*:v/v/')
    if [ ! "$version" ]; then
        echo "$image not found in $file" >&2
        exit 1
    fi
    echo -n "$version"
}

OIM_MALLOC_YAML="deploy/kubernetes/malloc/malloc-daemonset.yaml"

patch_file "https://raw.githubusercontent.com/kubernetes-csi/external-provisioner/$(image_version $OIM_MALLOC_YAML csi-provisioner)/deploy/kubernetes/rbac.yaml" oim-malloc >deploy/kubernetes/malloc/csi-provisioner-rbac.yaml
patch_file "https://raw.githubusercontent.com/kubernetes-csi/external-attacher/$(image_version $OIM_MALLOC_YAML csi-attacher)/deploy/kubernetes/rbac.yaml" oim-malloc >deploy/kubernetes/malloc/csi-attacher-rbac.yaml
