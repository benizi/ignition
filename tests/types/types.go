// Copyright 2017 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package types

import (
	"fmt"
)

const (
	RootDeviceImageFile = "blackbox_ignition_test.img"
)

type File struct {
	Node
	Contents string
	Mode     string
}

type Directory struct {
	Node
	Mode string
}

type Link struct {
	Node
	Target string
	Hard   bool
}

type Node struct {
	Name      string
	Directory string
	User      int
	Group     int
}

type Disk struct {
	ImageFile  string
	Partitions Partitions
}

type Partitions []*Partition

type Partition struct {
	Number          int
	Label           string
	TypeCode        string
	TypeGUID        string
	GUID            string
	Device          string
	Offset          int
	Length          int
	FilesystemType  string
	FilesystemLabel string
	FilesystemUUID  string
	MountPath       string
	Hybrid          bool
	Files           []File
	Directories     []Directory
	Links           []Link
	RemovedNodes    []Node
}

type MntDevice struct {
	Label        string
	Substitution string
}

type Test struct {
	Name       string
	In         []Disk
	Out        []Disk
	MntDevices []MntDevice
	Config     string
}

func (ps Partitions) GetPartition(label string) *Partition {
	for _, p := range ps {
		if p.Label == label {
			return p
		}
	}
	panic(fmt.Sprintf("couldn't find partition with label %q", label))
}

func (ps Partitions) AddFiles(label string, fs []File) {
	p := ps.GetPartition(label)
	p.Files = append(p.Files, fs...)
}

func (ps Partitions) AddDirectories(label string, ds []Directory) {
	p := ps.GetPartition(label)
	p.Directories = append(p.Directories, ds...)
}

func (ps Partitions) AddLinks(label string, ls []Link) {
	p := ps.GetPartition(label)
	p.Links = append(p.Links, ls...)
}

func (ps Partitions) AddRemovedNodes(label string, ns []Node) {
	p := ps.GetPartition(label)
	p.RemovedNodes = append(p.RemovedNodes, ns...)
}

func GetBaseDisk() []Disk {
	return []Disk{
		{
			ImageFile: RootDeviceImageFile,
			Partitions: Partitions{
				{
					Number:         1,
					Label:          "EFI-SYSTEM",
					TypeCode:       "efi",
					Length:         262144,
					FilesystemType: "vfat",
					Hybrid:         true,
					Files: []File{
						{
							Node: Node{
								Name:      "multiLine",
								Directory: "path/example",
							},
							Contents: "line 1\nline 2",
						}, {
							Node: Node{
								Name:      "singleLine",
								Directory: "another/path/example",
							},
							Contents: "single line",
						}, {
							Node: Node{
								Name:      "emptyFile",
								Directory: "empty",
							},
						}, {
							Node: Node{
								Name:      "noPath",
								Directory: "",
							},
						},
					},
				}, {
					Number:   2,
					Label:    "BIOS-BOOT",
					TypeCode: "bios",
					Length:   4096,
				}, {
					Number:         3,
					Label:          "USR-A",
					GUID:           "7130c94a-213a-4e5a-8e26-6cce9662f132",
					TypeCode:       "coreos-rootfs",
					Length:         2097152,
					FilesystemType: "ext2",
				}, {
					Number:   4,
					Label:    "USR-B",
					GUID:     "e03dd35c-7c2d-4a47-b3fe-27f15780a57c",
					TypeCode: "coreos-rootfs",
					Length:   2097152,
				}, {
					Number:   5,
					Label:    "ROOT-C",
					GUID:     "d82521b4-07ac-4f1c-8840-ddefedc332f3",
					TypeCode: "blank",
					Length:   0,
				}, {
					Number:         6,
					Label:          "OEM",
					TypeCode:       "data",
					Length:         262144,
					FilesystemType: "ext4",
				}, {
					Number:   7,
					Label:    "OEM-CONFIG",
					TypeCode: "coreos-reserved",
					Length:   131072,
				}, {
					Number:   8,
					Label:    "coreos-reserved",
					TypeCode: "blank",
					Length:   0,
				}, {
					Number:         9,
					Label:          "ROOT",
					TypeCode:       "coreos-resize",
					Length:         12943360,
					FilesystemType: "ext4",
				},
			},
		},
	}
}
