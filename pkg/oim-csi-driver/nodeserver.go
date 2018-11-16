/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gopkg.in/fsnotify/fsnotify.v1"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/mount"
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

	log.FromContext(ctx).Infow("mounting",
		"target", targetPath,
		"fstype", fsType,
		"read-only", readOnly,
		"volumeid", volumeID,
		"attributes", attrib,
		"flags", mountFlags,
	)

	device := ""
	if ns.od.vhostEndpoint != "" {
		if ns.od.emulate != nil {
			return nil, fmt.Errorf("emulating CSI driver %q not currently implemented without SPDK", ns.od.emulate.CSIDriverName)
		}

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
			log.FromContext(ctx).Infof("Reusing already started NBD disk: %s", nbdDevice)
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
		conn, err := ns.od.DialRegistry(ctx)
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		defer conn.Close()
		controllerClient := oim.NewControllerClient(conn)
		registryClient := oim.NewRegistryClient(conn)

		// Find out about configured PCI address before
		// triggering the more complex MapVolume operation.
		var defPCIAddress oim.PCIAddress
		path := ns.od.oimControllerID + "/" + oimcommon.RegistryPCI
		valuesReply, err := registryClient.GetValues(ctx, &oim.GetValuesRequest{
			Path: path,
		})
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("get PCI address from registry: %s", err))
		}
		if len(valuesReply.GetValues()) > 1 {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("expected at most one PCI address in registry at path %s: %s", path, valuesReply.GetValues()))
		}
		if len(valuesReply.GetValues()) == 1 {
			p, err := oimcommon.ParseBDFString(valuesReply.GetValues()[0].Value)
			if err != nil {
				return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("get PCI address from registry at path %s: %s", path, err))
			}
			defPCIAddress = *p
		}

		// Make volume available and/or find out where it is.
		ctx := metadata.AppendToOutgoingContext(ctx, "controllerid", ns.od.oimControllerID)
		request := &oim.MapVolumeRequest{
			VolumeId: volumeID,
			// Malloc BDev is the default. It takes no special parameters.
			Params: &oim.MapVolumeRequest_Malloc{
				Malloc: &oim.MallocParams{},
			},
		}
		if ns.od.emulate != nil {
			// Replace default parameters with the actual
			// values for the request. Interpretation of
			// the request depends on which CSI driver we
			// emulate.
			if err := ns.od.emulate.MapVolumeParams(req, request); err != nil {
				return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("create MapVolumeRequest parameters: %s", err))
			}
		}
		reply, err := controllerClient.MapVolume(ctx, request)
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("MapVolume for %s failed: %s", volumeID, err))
		}

		// Find device node based on reply. If the PCI address
		// is missing or incomplete, it must be set in the
		// registry.
		pciAddress := reply.GetPciAddress()
		if pciAddress == nil {
			pciAddress = &oim.PCIAddress{}
		}
		complete := oimcommon.CompletePCIAddress(*pciAddress, defPCIAddress)
		if complete.Domain == 0xFFFF {
			// We default the domain to zero because it
			// rarely needed. Everything else must be
			// specified.
			complete.Domain = 0
		}
		if complete.Bus == 0xFFFF || complete.Device == 0xFFFF || complete.Function == 0xFFFF {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("need complete PCI address with bus:device.function: %s from controller, %s from registry at path %s => combined %s",
				oimcommon.PrettyPCIAddress(pciAddress),
				oimcommon.PrettyPCIAddress(&defPCIAddress),
				oimcommon.PrettyPCIAddress(&complete),
				path))
		}

		dev, major, minor, err := waitForDevice(ctx, "/sys/dev/block", &complete, reply.GetScsiDisk())
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

func waitForDevice(ctx context.Context, sys string, pciAddress *oim.PCIAddress, scsiDisk *oim.SCSIDisk) (string, int, int, error) {
	log.FromContext(ctx).Infow("waiting for block device",
		"sys", sys,
		"PCI", pciAddress,
		"scsi", scsiDisk,
	)
	watcher, err := fsnotify.NewWatcher()
	watcher.Add(sys)
	if err != nil {
		return "", 0, 0, status.Error(codes.Internal, err.Error())
	}

	for {
		dev, major, minor, err := findDev(ctx, sys, pciAddress, scsiDisk)
		if err != nil {
			// None of the operations should have failed. Give up.
			return "", 0, 0, status.Error(codes.Internal, err.Error())
		}
		if dev != "" {
			return dev, major, minor, nil
		}
		select {
		case <-ctx.Done():
			return "", 0, 0, status.Errorf(codes.DeadlineExceeded, "timed out waiting for device %s, SCSI disk '%+v'",
				oimcommon.PrettyPCIAddress(pciAddress), scsiDisk)
		case <-watcher.Events:
			// Try again.
			log.FromContext(ctx).Debugw("changed",
				"sys", sys,
			)
		case <-time.After(5 * time.Second):
			// Sometimes inotify seems to miss events. Recover by checking from time to time.
			log.FromContext(ctx).Debugw("checking after timeout",
				"sys", sys,
			)
		case err := <-watcher.Errors:
			return "", 0, 0, status.Errorf(codes.Internal, "watching %s: %s", sys, err.Error())
		}
	}
}

var (
	majorMinor = regexp.MustCompile(`^(\d+):(\d+)$`)
	pciRe      = regexp.MustCompile(`/pci[0-9a-fA-F]{1,4}:[0-9a-fA-F]{1,2}/([0-9a-fA-F]{1,4}):([0-9a-fA-F]{1,2}):([0-9a-fA-F]{1,2})\.([0-7])/`)
	scsiRe     = regexp.MustCompile(`/target\d+:\d+:\d+/\d+:\d+:(\d+):(\d+)/block/`)
)

func extractPCIAddress(str string) (*oim.PCIAddress, string) {
	parts := pciRe.FindStringSubmatch(str)
	if len(parts) == 0 {
		return nil, str
	}
	remainder := strings.Replace(str, parts[0], "", 1)
	addr := &oim.PCIAddress{
		Domain:   oimcommon.HexToU32(parts[1]),
		Bus:      oimcommon.HexToU32(parts[2]),
		Device:   oimcommon.HexToU32(parts[3]),
		Function: oimcommon.HexToU32(parts[4]),
	}
	return addr, remainder
}

func extractSCSI(str string) *oim.SCSIDisk {
	parts := scsiRe.FindStringSubmatch(str)
	if len(parts) == 0 {
		return nil
	}
	return &oim.SCSIDisk{
		Target: oimcommon.HexToU32(parts[1]),
		Lun:    oimcommon.HexToU32(parts[2]),
	}
}

func findDev(ctx context.Context, sys string, pciAddress *oim.PCIAddress, scsiDisk *oim.SCSIDisk) (string, int, int, error) {
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
		// for PCI domain 0000, bus 00, device 15, function 9, SCSI target 7 and LUN 0.
		log.FromContext(ctx).Debugw("symlink",
			"from", fullpath,
			"to", target,
		)
		currentAddr, remainder := extractPCIAddress(target)
		if currentAddr == nil || *currentAddr != *pciAddress {
			continue
		}
		if scsiDisk != nil {
			currentSCSI := extractSCSI(remainder)
			if *currentSCSI != *scsiDisk {
				continue
			}
		}
		// Because Readdir sorted the entries, we are guaranteed to find
		// the main block device before its partitions (i.e. 8:0 before 8:1).
		sep := strings.LastIndex(target, block)
		if sep != -1 {
			dev := target[sep+len(block):]
			log.FromContext(ctx).Debugw("found block device",
				"entry", entry.Name(),
				"dev", dev,
			)
			parts := majorMinor.FindStringSubmatch(entry.Name())
			if parts == nil {
				return "", 0, 0, fmt.Errorf("Unexpected entry in %s, not a major:minor symlink: %s", sys, entry.Name())
			}
			major, _ := strconv.Atoi(parts[1])
			minor, _ := strconv.Atoi(parts[2])
			return dev, major, minor, nil
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
	log.FromContext(ctx).Infow("unmount",
		"target", targetPath,
		"volumeid", volumeID,
	)
	if err := mount.New("").Unmount(req.GetTargetPath()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

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
		conn, err := ns.od.DialRegistry(ctx)
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
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

func (ns *nodeServer) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return &csi.NodeGetVolumeStatsResponse{}, nil
}
