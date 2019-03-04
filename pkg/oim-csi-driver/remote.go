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

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"gopkg.in/fsnotify/fsnotify.v1"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/spec/oim/v0"
)

type remoteSPDK struct {
	oimRegistryAddress string
	registryCA         string
	registryKey        string
	oimControllerID    string

	mapVolumeParams func(request interface{}, to *oim.MapVolumeRequest) error
}

var _ OIMBackend = &remoteSPDK{}

func (r *remoteSPDK) createVolume(ctx context.Context, volumeID string, requiredBytes int64) (int64, error) {
	// Check for maximum available capacity
	capacity := requiredBytes
	if capacity >= maxStorageCapacity {
		return 0, status.Errorf(codes.OutOfRange, "Requested capacity %d exceeds maximum allowed %d", capacity, maxStorageCapacity)
	}

	if capacity == 0 {
		// If capacity is unset, round up to minimum size (1MB?).
		capacity = mib
	} else {
		// Round up to multiple of 512.
		capacity = (capacity + 511) / 512 * 512
	}

	if err := r.provision(ctx, volumeID, capacity); err != nil {
		return 0, err
	}

	return capacity, nil
}

func (r *remoteSPDK) deleteVolume(ctx context.Context, volumeID string) error {
	return r.provision(ctx, volumeID, 0)
}

func (r *remoteSPDK) provision(ctx context.Context, bdevName string, size int64) error {
	// Connect to OIM controller through OIM registry.
	conn, err := r.dialRegistry(ctx)
	if err != nil {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	defer conn.Close()
	controllerClient := oim.NewControllerClient(conn)
	ctx = metadata.AppendToOutgoingContext(ctx, "controllerid", r.oimControllerID)
	_, err = controllerClient.ProvisionMallocBDev(ctx, &oim.ProvisionMallocBDevRequest{
		BdevName: bdevName,
		Size_:    size,
	})
	return err
}

func (r *remoteSPDK) checkVolumeExists(ctx context.Context, volumeID string) error {
	// Connect to OIM controller through OIM registry.
	conn, err := r.dialRegistry(ctx)
	if err != nil {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	defer conn.Close()
	controllerClient := oim.NewControllerClient(conn)
	ctx = metadata.AppendToOutgoingContext(ctx, "controllerid", r.oimControllerID)
	_, err = controllerClient.CheckMallocBDev(ctx, &oim.CheckMallocBDevRequest{
		BdevName: volumeID,
	})
	return err
}

func (r *remoteSPDK) dialRegistry(ctx context.Context) (*grpc.ClientConn, error) {
	// Intentionally loaded anew for each connection attempt.
	// File content can change over time.
	transportCreds, err := oimcommon.LoadTLS(r.registryCA, r.registryKey, "component.registry")
	if err != nil {
		return nil, errors.Wrap(err, "load TLS certs")
	}
	opts := oimcommon.ChooseDialOpts(r.oimRegistryAddress, grpc.WithTransportCredentials(transportCreds))
	conn, err := grpc.Dial(r.oimRegistryAddress, opts...)
	if err != nil {
		return nil, errors.Wrapf(err, "connect to OIM registry at %s", r.oimRegistryAddress)
	}
	return conn, nil
}

func (r *remoteSPDK) createDevice(ctx context.Context, volumeID string, csiRequest interface{}) (string, cleanup, error) {
	// Connect to OIM controller through OIM registry.
	conn, err := r.dialRegistry(ctx)
	if err != nil {
		return "", nil, errors.Wrap(err, "connect to OIM registry")
	}
	defer conn.Close()
	controllerClient := oim.NewControllerClient(conn)
	registryClient := oim.NewRegistryClient(conn)

	// Find out about configured PCI address before
	// triggering the more complex MapVolume operation.
	var defPCIAddress oim.PCIAddress
	path := r.oimControllerID + "/" + oimcommon.RegistryPCI
	valuesReply, err := registryClient.GetValues(ctx, &oim.GetValuesRequest{
		Path: path,
	})
	if err != nil {
		return "", nil, errors.Wrap(err, "get PCI address from registry")
	}
	if len(valuesReply.GetValues()) > 1 {
		return "", nil, errors.Errorf("expected at most one PCI address in registry at path %s: %s", path, valuesReply.GetValues())
	}
	if len(valuesReply.GetValues()) == 1 {
		p, err := oimcommon.ParseBDFString(valuesReply.GetValues()[0].Value)
		if err != nil {
			return "", nil, errors.Wrapf(err, "get PCI address from registry at path %s", path)
		}
		defPCIAddress = *p
	}

	// Make volume available and/or find out where it is.
	ctx = metadata.AppendToOutgoingContext(ctx, "controllerid", r.oimControllerID)
	request := &oim.MapVolumeRequest{
		VolumeId: volumeID,
		// Malloc BDev is the default. It takes no special parameters.
		Params: &oim.MapVolumeRequest_Malloc{
			Malloc: &oim.MallocParams{},
		},
	}
	if r.mapVolumeParams != nil {
		// Replace default parameters with the actual
		// values for the request. Interpretation of
		// the request depends on which CSI driver we
		// emulate.
		if err := r.mapVolumeParams(csiRequest, request); err != nil {
			return "", nil, errors.Wrap(err, "create MapVolumeRequest parameters")
		}
	}
	reply, err := controllerClient.MapVolume(ctx, request)
	if err != nil {
		return "", nil, errors.Wrapf(err, "MapVolume for %s", volumeID)
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
		return "", nil, errors.Errorf("need complete PCI address with bus:device.function: %s from controller, %s from registry at path %s => combined %s",
			oimcommon.PrettyPCIAddress(pciAddress),
			oimcommon.PrettyPCIAddress(&defPCIAddress),
			oimcommon.PrettyPCIAddress(&complete),
			path)
	}

	dev, major, minor, err := waitForDevice(ctx, "/sys/dev/block", &complete, reply.GetScsiDisk())
	if err != nil {
		return "", nil, errors.Wrap(err, "wait for device")
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
		return "", nil, errors.Wrap(err, "temp dir")
	}
	devNode := filepath.Join(tmpDir, dev)
	cleanup := func() error {
		return os.RemoveAll(tmpDir)
	}
	if err := syscall.Mknod(devNode, syscall.S_IFBLK|0666, makedev(major, minor)); err != nil && !os.IsExist(err) {
		return "", cleanup, errors.Wrap(err, "mknod")
	}
	return devNode, cleanup, nil
}

func (r *remoteSPDK) deleteDevice(ctx context.Context, volumeID string) error {
	// Connect to OIM controller through OIM registry.
	conn, err := r.dialRegistry(ctx)
	if err != nil {
		return errors.Wrap(err, "connect to registry")
	}
	controllerClient := oim.NewControllerClient(conn)

	// Make volume available and/or find out where it is.
	ctx = metadata.AppendToOutgoingContext(ctx, "controllerid", r.oimControllerID)
	if _, err := controllerClient.UnmapVolume(ctx, &oim.UnmapVolumeRequest{
		VolumeId: volumeID,
	}); err != nil {
		return errors.Wrapf(err, "UnmapVolume for %s", volumeID)
	}
	return nil
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
	if err == nil {
		err = watcher.Add(sys)
	}
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
			// The regex has already ensured that we have a valid integer.
			// nolint: gosec
			major, _ := strconv.Atoi(parts[1])
			minor, _ := strconv.Atoi(parts[2])
			return dev, major, minor, nil
		}
	}
	return "", 0, 0, nil
}
