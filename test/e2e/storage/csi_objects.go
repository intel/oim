/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package storage

import (
	"flag"
)

var csiImageVersions = map[string]string{
	"hostpathplugin":   "v0.2.0",
	"csi-attacher":     "v0.2.0",
	"csi-provisioner":  "v0.2.1",
	"driver-registrar": "v0.2.0",
}

var (
	csiImageVersion  = flag.String("csiImageVersion", "", "overrides the default tag used for hostpathplugin/csi-attacher/csi-provisioner/driver-registrar images")
	csiImageRegistry = flag.String("csiImageRegistry", "quay.io/k8scsi", "overrides the default repository used for hostpathplugin/csi-attacher/csi-provisioner/driver-registrar images")
)

func csiContainerImage(image string) string {
	var fullName string
	fullName += *csiImageRegistry + "/" + image + ":"
	if *csiImageVersion != "" {
		fullName += *csiImageVersion
	} else {
		fullName += csiImageVersions[image]
	}
	return fullName
}
