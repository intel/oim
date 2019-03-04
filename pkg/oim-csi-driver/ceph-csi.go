/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"fmt"
	"strings"

	csi0 "github.com/intel/oim/pkg/spec/csi/v0"
	"github.com/intel/oim/pkg/spec/oim/v0"
)

const (
	rbdDefaultAdminID = "admin"
	rbdDefaultUserID  = rbdDefaultAdminID
)

type rbdVolume struct {
	VolName            string `json:"volName"`
	VolID              string `json:"volID"`
	Monitors           string `json:"monitors"`
	MonValueFromSecret string `json:"monValueFromSecret"`
	Pool               string `json:"pool"`
	AdminID            string `json:"adminId"`
	UserID             string `json:"userId"`
}

var emulateCephCSI0 = &EmulateCSI0Driver{
	CSIDriverName: "ceph-csi",
	// from https://github.com/ceph/ceph-csi/blob/master/pkg/rbd/rbd.go
	ControllerServiceCapabilities: []csi0.ControllerServiceCapability_RPC_Type{
		csi0.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi0.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi0.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		csi0.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
	},
	VolumeCapabilityAccessModes: []csi0.VolumeCapability_AccessMode_Mode{csi0.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
	MapVolumeParams:             mapCephVolumeParams,
}

func init() {
	supportedCSI0Drivers["ceph-csi"] = emulateCephCSI0
}

func mapCephVolumeParams(from *csi0.NodeStageVolumeRequest, to *oim.MapVolumeRequest) error {
	// Currently ceph-csi is passed this kind of request:
	//
	// volume_id: ".....-0242ac110002"
	// target_path:"/var/lib/kubelet/pods/.../mount"
	// volume_capability:<mount:<fs_type:"ext4" > access_mode:<mode:SINGLE_NODE_WRITER > >
	// node_publish_secrets:<key:"admin" value:"AQAOLsdbXztfHBAAul7+rC3JCVIC7HdjAe27yA==\n" >
	// node_publish_secrets:<key:"kubernetes" value:"AQAPLsdbtoIaGBAAUbWXz2Y+dw3Lo5mPpvRa6g==\n" >
	// node_publish_secrets:<key:"monitors" value:"192.168.7.2:6789,192.168.7.4:6789,192.168.7.6:6789,192.168.7.8:6789" >
	// volume_attributes:<key:"adminid" value:"admin" >
	// volume_attributes:<key:"csiNodePublishSecretName" value:"csi-rbd-secret" >
	// volume_attributes:<key:"csiNodePublishSecretNamespace" value:"default" >
	// volume_attributes:<key:"csiProvisionerSecretName" value:"csi-rbd-secret" >
	// volume_attributes:<key:"csiProvisionerSecretNamespace" value:"default" >
	// volume_attributes:<key:"monValueFromSecret" value:"monitors" >
	// volume_attributes:<key:"pool" value:"rbd" >
	// volume_attributes:<key:"storage.kubernetes.io/csiProvisionerIdentity" value:"1539780484677-8081-oim-rbd" >
	// volume_attributes:<key:"userid" value:"kubernetes" >
	//
	// The volume attributes are documented in https://github.com/ceph/ceph-csi/blob/master/docs/deploy-rbd.md#configuration
	//
	// The code for retrieving the relevant attributes was copied from https://github.com/ceph/ceph-csi/tree/master/pkg/rbd

	targetPath := from.GetStagingTargetPath()
	if !strings.HasSuffix(targetPath, "/globalmount") {
		return fmt.Errorf("malformed value of target path: %s", targetPath)
	}
	s := strings.Split(strings.TrimSuffix(targetPath, "/globalmount"), "/")
	volName := s[len(s)-1]

	volOptions, err := getRBDVolumeOptions(from.VolumeAttributes)
	if err != nil {
		return err
	}
	userID := volOptions.UserID
	credentials := from.GetNodeStageSecrets()

	mon, err := getMon(volOptions, credentials)
	if err != nil {
		return err
	}

	key, err := getRBDKey(userID, credentials)
	if err != nil {
		return err
	}

	to.Params = &oim.MapVolumeRequest_Ceph{
		Ceph: &oim.CephParams{
			UserId:   userID,
			Secret:   key,
			Monitors: mon,
			Pool:     volOptions.Pool,
			Image:    volName,
		},
	}
	return nil
}

func getRBDVolumeOptions(volOptions map[string]string) (*rbdVolume, error) {
	var ok bool
	rbdVol := &rbdVolume{}
	rbdVol.Pool, ok = volOptions["pool"]
	if !ok {
		return nil, fmt.Errorf("Missing required parameter pool")
	}
	rbdVol.Monitors, ok = volOptions["monitors"]
	if !ok {
		// if mons are not set in options, check if they are set in secret
		if rbdVol.MonValueFromSecret, ok = volOptions["monValueFromSecret"]; !ok {
			return nil, fmt.Errorf("Either monitors or monValueFromSecret must be set")
		}
	}
	rbdVol.AdminID, ok = volOptions["adminid"]
	if !ok {
		rbdVol.AdminID = rbdDefaultAdminID
	}
	rbdVol.UserID, ok = volOptions["userid"]
	if !ok {
		rbdVol.UserID = rbdDefaultUserID
	}
	return rbdVol, nil
}

func getRBDKey(id string, credentials map[string]string) (string, error) {

	if key, ok := credentials[id]; ok {
		return key, nil
	}
	return "", fmt.Errorf("RBD key for ID: %s not found", id)
}

func getMon(pOpts *rbdVolume, credentials map[string]string) (string, error) {
	mon := pOpts.Monitors
	if len(mon) == 0 {
		// if mons are set in secret, retrieve them
		if len(pOpts.MonValueFromSecret) == 0 {
			// yet another sanity check
			return "", fmt.Errorf("either monitors or monValueFromSecret must be set")
		}
		val, ok := credentials[pOpts.MonValueFromSecret]
		if !ok {
			return "", fmt.Errorf("mon data %s is not set in secret", pOpts.MonValueFromSecret)
		}
		mon = val
	}
	return mon, nil
}
