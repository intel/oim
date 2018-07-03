/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gopkg.in/fsnotify/fsnotify.v1"
	"k8s.io/kubernetes/pkg/util/mount"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/spdk"
	"github.com/intel/oim/pkg/spec/oim/v0"
)

type nodeServer struct {
	*DefaultNodeServer
	od *oimDriver
}

func findNBDDevice(ctx context.Context, client *spdk.Client, volumeID string) (nbdDevice string, err error) {
	nbdDisks, err := spdk.GetNBDDisks(ctx, client)
	if err != nil {
		return "", status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to get NDB disks from SPDK: %s", err))
	}
	for _, nbd := range nbdDisks {
		if nbd.BDevName == volumeID {
			return nbd.NBDDevice, nil
		}
	}
	return "", nil
}

func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	// Check arguments
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability missing in request")
	}
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	// Check and prepare mount point.
	targetPath := req.GetTargetPath()
	notMnt, err := mount.New("").IsLikelyNotMountPoint(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err = os.MkdirAll(targetPath, 0750); err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			notMnt = true
		} else {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	if !notMnt {
		// Already mounted, nothing to do.
		return &csi.NodePublishVolumeResponse{}, nil
	}

	fsType := req.GetVolumeCapability().GetMount().GetFsType()
	readOnly := req.GetReadonly()
	volumeID := req.GetVolumeId()
	attrib := req.GetVolumeAttributes()
	mountFlags := req.GetVolumeCapability().GetMount().GetMountFlags()

	oimcommon.Infof(4, ctx, "target %v\nfstype %v\nreadonly %v\nattributes %v\n mountflags %v\n",
		targetPath, fsType, readOnly, volumeID, attrib, mountFlags)

	device := ""
	if ns.od.vhostEndpoint != "" {
		// Connect to SPDK.
		client, err := spdk.New(ns.od.vhostEndpoint)
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to connect to SPDK: %s", err))
		}
		defer client.Close()

		// We might have already mapped that BDev to a NBD disk - check!
		nbdDevice, err := findNBDDevice(ctx, client, volumeID)
		if err != nil {
			return nil, err
		}
		if nbdDevice != "" {
			oimcommon.Infof(4, ctx, "Reusing already started NBD disk: %s", nbdDevice)
		} else {
			var nbdError error
			// Find a free NBD device node and start a NBD disk there.
			// Unfortunately this is racy. We assume that we are the
			// only users of /dev/nbd*.
			for i := 0; ; i++ {
				n := fmt.Sprintf("/dev/nbd%d", i)
				nbdFile, err := os.Open(n)
				// We stop when we run into the first non-existent device name.
				if os.IsNotExist(err) {
					if nbdError == nil {
						nbdError = err
					}
					break
				}
				if err != nil {
					nbdError = err
					continue
				}
				defer nbdFile.Close()
				size, err := oimcommon.GetBlkSize64(nbdFile)
				nbdFile.Close()
				if err != nil {
					nbdError = err
					continue
				}
				if size == 0 {
					// Seems unused, take it.
					nbdDevice = n
					break
				}
			}
			// Still nothing?!
			if nbdDevice == "" {
				return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to find an unused /dev/nbd*: %s", nbdError))
			}
		}

		args := spdk.StartNBDDiskArgs{
			BDevName:  volumeID,
			NBDDevice: nbdDevice,
		}
		if err := spdk.StartNBDDisk(ctx, client, args); err != nil {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to start SPDK NBD disk %+v: %s", args, err))
		}

		device = nbdDevice
	} else {
		// Connect to OIM controller through OIM registry.
		opts := oimcommon.ChooseDialOpts(ns.od.oimRegistryAddress)
		conn, err := grpc.Dial(ns.od.oimRegistryAddress, opts...)
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to connect to OIM registry at %s: %s", ns.od.oimRegistryAddress, err))
		}
		controllerClient := oim.NewControllerClient(conn)

		// Make volume available and/or find out where it is.
		ctx := metadata.AppendToOutgoingContext(ctx, "controllerid", ns.od.oimControllerID)
		reply, err := controllerClient.MapVolume(ctx, &oim.MapVolumeRequest{
			VolumeId: volumeID,
			// TODO: map attrib to params
			// For now we assume that we map a Malloc BDev with the same name.
			Params: &oim.MapVolumeRequest_Malloc{
				Malloc: &oim.MallocParams{},
			},
		})
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("MapVolume for %s failed: %s", volumeID, err))
		}

		// Find device node based on reply.
		dev, major, minor, err := waitForDevice(ctx, "/sys/dev/block", reply.GetDevice(), reply.GetScsi())
		if err != nil {
			return nil, err
		}

		// The actual /dev folder might not have the device,
		// for example when we run in a Docker container where
		// /dev was populated at startup time. Therefore we
		// create a temporary block special file. This has
		// to be under /dev instead of /tmp, because /tmp
		// might have been mounted with nodev, preventing
		// the usage of block devices there.
		tmpDir, err := ioutil.TempDir("/dev", dev)
		if err != nil {
			return nil, err
		}
		devNode := filepath.Join(tmpDir, dev)
		defer os.RemoveAll(tmpDir)
		if err := syscall.Mknod(devNode, syscall.S_IFBLK|0666, makedev(major, minor)); err != nil && !os.IsExist(err) {
			return nil, err
		}
		device = devNode
	}

	options := []string{}
	if readOnly {
		options = append(options, "ro")
	}
	diskMounter := &mount.SafeFormatAndMount{Interface: mount.New(""), Exec: mount.NewOsExec()}
	if err := diskMounter.FormatAndMount(device, targetPath, fsType, options); err != nil {
		// We get a pretty bad error code from FormatAndMount ("exit code 1") :-/
		return nil, errors.Wrapf(err, "formatting as %s and mounting %s at %s", fsType, device, targetPath)
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

// makedev prepares the dev argument for Mknod.
func makedev(major int, minor int) int {
	// Formular from https://github.com/lattera/glibc/blob/master/sysdeps/unix/sysv/linux/makedev.c
	// In contrast to glibc, Go only uses int instead of unsigned long long.
	return (minor & 0xff) | ((major & 0xfff) << 8) |
		((minor &^ 0xff) << 12) |
		((major &^ 0xfff) << 32)
}

const (
	block = "/block/"
)

func waitForDevice(ctx context.Context, sys, blockDev, blockSCSI string) (string, int, int, error) {
	log.Printf("Waiting for %s entry with substring '%s' and SCSI unit '%s'", sys, blockDev, blockSCSI)
	watcher, err := fsnotify.NewWatcher()
	watcher.Add(sys)
	if err != nil {
		return "", 0, 0, status.Error(codes.Internal, err.Error())
	}

	// If the overall call has a deadline, then stop waiting
	// slightly before that so that we still have a chance to
	// return a proper error.
	deadline, ok := ctx.Deadline()
	var doneCtx context.Context
	if ok {
		deadlineCtx, cancel := context.WithDeadline(ctx, deadline.Add(-1*time.Second))
		defer cancel()
		doneCtx = deadlineCtx
	} else {
		doneCtx = ctx
	}

	for {
		dev, major, minor, err := findDev(sys, blockDev, blockSCSI)
		if err != nil {
			// None of the operations should have failed. Give up.
			return "", 0, 0, status.Error(codes.Internal, err.Error())
		}
		if dev != "" {
			return dev, major, minor, nil
		}
		select {
		case <-doneCtx.Done():
			return "", 0, 0, status.Errorf(codes.DeadlineExceeded, "timed out waiting for device '%s', SCSI unit '%s'", blockDev, blockSCSI)
		case <-watcher.Events:
			// Try again.
			log.Printf("%s changed.", sys)
		case err := <-watcher.Errors:
			return "", 0, 0, status.Errorf(codes.Internal, "watching %s: %s", sys, err.Error())
		}
	}
}

var (
	majorMinor = regexp.MustCompile("^(\\d+):(\\d+)$")
)

func findDev(sys, blockDev, blockSCSI string) (string, int, int, error) {
	files, err := ioutil.ReadDir(sys)
	if err != nil {
		return "", 0, 0, err
	}
	for _, entry := range files {
		fullpath := filepath.Join(sys, entry.Name())
		target, err := os.Readlink(fullpath)
		if err != nil {
			return "", 0, 0, err
		}
		// target is expected to have this format:
		// ../../devices/pci0000:00/0000:00:15.0/virtio3/host0/target0:0:7/0:0:7:0/block/sda
		// for PCI address 0x15, SCSI target 7 and LUN 0.
		log.Printf("%s -> %s", fullpath, target)
		if strings.Contains(target, blockDev) &&
			(blockSCSI == "" || strings.Contains(target, ":"+blockSCSI+block)) {
			// Because Readdir sorted the entries, we are guaranteed to find
			// the main block device before its partitions (i.e. 8:0 before 8:1).
			sep := strings.LastIndex(target, block)
			if sep != -1 {
				dev := target[sep+len(block):]
				log.Printf("Found block device %s = %s", entry.Name(), dev)
				parts := majorMinor.FindStringSubmatch(entry.Name())
				if parts == nil {
					return "", 0, 0, fmt.Errorf("Unexpected entry in %s, not a major:minor symlink: %s", sys, entry.Name())
				}
				major, _ := strconv.Atoi(parts[1])
				minor, _ := strconv.Atoi(parts[2])
				return dev, major, minor, nil
			}
		}
	}
	return "", 0, 0, nil
}

func (ns *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}
	targetPath := req.GetTargetPath()
	volumeID := req.GetVolumeId()

	// Unmounting the image
	// TODO: check whether this really is still a mount point. We might have removed it already.
	if err := mount.New("").Unmount(req.GetTargetPath()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	oimcommon.Infof(4, ctx, "volume %s/%s has been unmounted.", targetPath, volumeID)

	if ns.od.vhostEndpoint != "" {
		// Connect to SPDK.
		client, err := spdk.New(ns.od.vhostEndpoint)
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to connect to SPDK: %s", err))
		}
		defer client.Close()

		// Stop NBD disk.
		nbdDevice, err := findNBDDevice(ctx, client, volumeID)
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to get NDB disks from SPDK: %s", err))
		}
		args := spdk.StopNBDDiskArgs{NBDDevice: nbdDevice}
		if err := spdk.StopNBDDisk(ctx, client, args); err != nil {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to stop SPDK NDB disk %+v: %s", args, err))
		}
	} else {
		// Connect to OIM controller through OIM registry.
		opts := oimcommon.ChooseDialOpts(ns.od.oimRegistryAddress)
		conn, err := grpc.Dial(ns.od.oimRegistryAddress, opts...)
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to connect to OIM registry at %s: %s", ns.od.oimRegistryAddress, err))
		}
		controllerClient := oim.NewControllerClient(conn)

		// Make volume available and/or find out where it is.
		ctx := metadata.AppendToOutgoingContext(ctx, "controllerid", ns.od.oimControllerID)
		if _, err := controllerClient.UnmapVolume(ctx, &oim.UnmapVolumeRequest{
			VolumeId: volumeID,
		}); err != nil {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("UnmapVolume for %s failed: %s", volumeID, err))
		}
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetStagingTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetStagingTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}
