/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

// spdk provides Go bindings for the SPDK JSON 2.0 RPC interface
// (http://www.spdk.io/doc/jsonrpc.html).
package spdk

import (
	"context"
)

type GetBDevsArgs struct {
	Name string `json:"name,omitempty"`
}

type SupportedIOTypes struct {
	Read       bool `json:"read"`
	Write      bool `json:"write"`
	Unmap      bool `json:"unmap"`
	WriteZeros bool `json:"write_zeroes"`
	Flush      bool `json:"flush"`
	Reset      bool `json:"reset"`
	NVMEAdmin  bool `json:"nvme_admin"`
	NVMEIO     bool `json:"nvme_io"`
}

type BDev struct {
	Name             string           `json:"name"`
	ProductName      string           `json:"product_name"`
	UUID             string           `json:"uuid"`
	BlockSize        int64            `json:"block_size"`
	NumBlocks        int64            `json:"num_blocks"`
	Claimed          bool             `json:"claimed"`
	SupportedIOTypes SupportedIOTypes `json:"supported_io_types"`
}

type GetBDevsResponse []BDev

func GetBDevs(ctx context.Context, client *Client, args GetBDevsArgs) (GetBDevsResponse, error) {
	var response GetBDevsResponse
	err := client.Invoke(ctx, "get_bdevs", args, &response)
	if err != nil {
		return nil, err
	}
	return response, nil
}

type DeleteBDevArgs struct {
	Name string `json:"name"`
}

func DeleteBDev(ctx context.Context, client *Client, args DeleteBDevArgs) error {
	return client.Invoke(ctx, "delete_bdev", args, nil)
}

type ConstructBDevArgs struct {
	NumBlocks int64  `json:"num_blocks"`
	BlockSize int64  `json:"block_size"`
	Name      string `json:"name,omitempty"`
	UUID      string `json:"uuid,omitempty"`
}

type ConstructBDevResponse string

type ConstructMallocBDevArgs struct {
	ConstructBDevArgs
}

func ConstructMallocBDev(ctx context.Context, client *Client, args ConstructMallocBDevArgs) (ConstructBDevResponse, error) {
	var response ConstructBDevResponse
	err := client.Invoke(ctx, "construct_malloc_bdev", args, &response)
	return response, err
}

type ConstructRBDBDevArgs struct {
	BlockSize int64             `json:"block_size"`
	Name      string            `json:"name,omitempty"`
	UserID    string            `json:"user_id,omitempty"`
	PoolName  string            `json:"pool_name"`
	RBDName   string            `json:"rbd_name"`
	Config    map[string]string `json:"config,omitempty"`
}

func ConstructRBDBDev(ctx context.Context, client *Client, args ConstructRBDBDevArgs) (ConstructBDevResponse, error) {
	var response ConstructBDevResponse
	err := client.Invoke(ctx, "construct_rbd_bdev", args, &response)
	return response, err
}

type StartNBDDiskArgs struct {
	BDevName  string `json:"bdev_name"`
	NBDDevice string `json:"nbd_device"`
}

func StartNBDDisk(ctx context.Context, client *Client, args StartNBDDiskArgs) error {
	return client.Invoke(ctx, "start_nbd_disk", args, nil)
}

type GetNBDDisksResponse []StartNBDDiskArgs

func GetNBDDisks(ctx context.Context, client *Client) (GetNBDDisksResponse, error) {
	var response GetNBDDisksResponse
	err := client.Invoke(ctx, "get_nbd_disks", nil, &response)
	return response, err
}

type StopNBDDiskArgs struct {
	NBDDevice string `json:"nbd_device"`
}

func StopNBDDisk(ctx context.Context, client *Client, args StopNBDDiskArgs) error {
	return client.Invoke(ctx, "stop_nbd_disk", args, nil)
}

type ConstructVHostSCSIControllerArgs struct {
	CPUMask    string `json:"cpumask,omitempty"`
	Controller string `json:"ctrlr"`
}

func ConstructVHostSCSIController(ctx context.Context, client *Client, args ConstructVHostSCSIControllerArgs) error {
	return client.Invoke(ctx, "construct_vhost_scsi_controller", args, nil)
}

type AddVHostSCSILUNArgs struct {
	Controller    string `json:"ctrlr"`
	SCSITargetNum uint32 `json:"scsi_target_num"`
	BDevName      string `json:"bdev_name"`
}

func AddVHostSCSILUN(ctx context.Context, client *Client, args AddVHostSCSILUNArgs) error {
	return client.Invoke(ctx, "add_vhost_scsi_lun", args, nil)
}

type RemoveVHostSCSITargetArgs struct {
	Controller    string `json:"ctrlr"`
	SCSITargetNum uint32 `json:"scsi_target_num"`
}

func RemoveVHostSCSITarget(ctx context.Context, client *Client, args RemoveVHostSCSITargetArgs) error {
	return client.Invoke(ctx, "remove_vhost_scsi_target", args, nil)
}

type RemoveVHostControllerArgs struct {
	Controller string `json:"ctrlr"`
}

func RemoveVHostController(ctx context.Context, client *Client, args RemoveVHostControllerArgs) error {
	return client.Invoke(ctx, "remove_vhost_controller", args, nil)
}

type GetVHostControllersResponse []Controller

type Controller struct {
	Controller string `json:"ctrlr"`
	CPUMask    string `json:"cpumask"`
	// BackendSpecific holds the parsed JSON response for known
	// backends (like SCSIControllerSpecific), otherwise
	// the JSON data converted to basic types (map, list, etc.)
	BackendSpecific BackendSpecificType `json:"backend_specific"`
}

type BackendSpecificType map[string]interface{}
type SCSIControllerSpecific []SCSIControllerTarget
type SCSIControllerTarget struct {
	TargetName string
	LUNs       []SCSIControllerLUN
	ID         int32
	SCSIDevNum uint32
}
type SCSIControllerLUN struct {
	LUN      int32
	BDevName string
}

// getSCSIBackendSpecific interprets the Controller.BackendSpecific value for
// map entries with key "scsi". See https://github.com/spdk/spdk/issues/329#issuecomment-396266197
// and spdk_vhost_scsi_dump_info_json().
func getSCSIBackendSpecific(in interface{}) SCSIControllerSpecific {
	result := SCSIControllerSpecific{}
	list, ok := in.([]interface{})
	if !ok {
		return result
	}
	for _, entry := range list {
		if hash, ok := entry.(map[string]interface{}); ok {
			target := SCSIControllerTarget{
				LUNs: []SCSIControllerLUN{},
			}
			for key, value := range hash {
				switch key {
				case "target_name":
					if name, ok := value.(string); ok {
						target.TargetName = name
					}
				case "id":
					if id, ok := value.(float64); ok {
						target.ID = int32(id)
					}
				case "scsi_dev_num":
					if devNum, ok := value.(float64); ok {
						target.SCSIDevNum = uint32(devNum)
					}
				case "luns":
					if luns, ok := value.([]interface{}); ok {
						for _, lun := range luns {
							var l SCSIControllerLUN
							if hash, ok := lun.(map[string]interface{}); ok {
								for key, value := range hash {
									switch key {
									case "id":
										if id, ok := value.(float64); ok {
											l.LUN = int32(id)
										}
									case "bdev_name":
										if name, ok := value.(string); ok {
											l.BDevName = name
										}
									}
								}
							}
							target.LUNs = append(target.LUNs, l)
						}
					}
				}
			}
			result = append(result, target)
		}
	}
	return result
}

func GetVHostControllers(ctx context.Context, client *Client) (GetVHostControllersResponse, error) {
	var response GetVHostControllersResponse
	err := client.Invoke(ctx, "get_vhost_controllers", nil, &response)
	if err == nil {
		for _, controller := range response {
			for backend, specific := range controller.BackendSpecific {
				switch backend {
				case "scsi":
					controller.BackendSpecific[backend] = getSCSIBackendSpecific(specific)
				}
			}
		}
	}
	return response, err
}
