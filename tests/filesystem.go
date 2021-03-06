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

package blackbox

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/coreos/ignition/tests/types"
)

func mustRun(t *testing.T, command string, args ...string) []byte {
	out, err := exec.Command(command, args...).CombinedOutput()
	if err != nil {
		t.Fatal(command, err, string(out))
	}
	return out
}

func prepareRootPartitionForPasswd(t *testing.T, partitions []*types.Partition) {
	for _, p := range partitions {
		if p.Label == "ROOT" {
			dirs := []string{
				filepath.Join(p.MountPath, "home"),
				filepath.Join(p.MountPath, "usr", "bin"),
				filepath.Join(p.MountPath, "usr", "sbin"),
				filepath.Join(p.MountPath, "usr", "lib64"),
				filepath.Join(p.MountPath, "etc"),
			}
			for _, dir := range dirs {
				_ = os.MkdirAll(dir, 0755)
			}

			symlinks := []string{"lib64", "bin", "sbin"}
			for _, symlink := range symlinks {
				_ = os.Symlink(
					filepath.Join(p.MountPath, "usr", symlink),
					filepath.Join(p.MountPath, symlink))
			}

			copies := [][2]string{
				{"/etc/ld.so.cache", filepath.Join(p.MountPath, "etc")},
				{"/lib64/libblkid.so.1", filepath.Join(p.MountPath, "usr", "lib64")},
				{"/lib64/libpthread.so.0", filepath.Join(p.MountPath, "usr", "lib64")},
				{"/lib64/libc.so.6", filepath.Join(p.MountPath, "usr", "lib64")},
				{"/lib64/libuuid.so.1", filepath.Join(p.MountPath, "usr", "lib64")},
				{"/lib64/ld-linux-x86-64.so.2", filepath.Join(p.MountPath, "usr", "lib64")},
				{"/lib64/libnss_files.so.2", filepath.Join(p.MountPath, "usr", "lib64")},
				{"bin/amd64/id", filepath.Join(p.MountPath, "usr", "bin")},
				{"bin/amd64/useradd", filepath.Join(p.MountPath, "usr", "sbin")},
				{"bin/amd64/usermod", filepath.Join(p.MountPath, "usr", "sbin")},
			}

			for _, cp := range copies {
				_, _ = exec.Command("cp", cp[0], cp[1]).CombinedOutput()
			}
		}
	}
}

func getRootLocation(partitions []*types.Partition) string {
	for _, p := range partitions {
		if p.Label == "ROOT" {
			return p.MountPath
		}
	}
	return ""
}

func removeFile(t *testing.T, imageFile string) {
	err := os.Remove(imageFile)
	if err != nil {
		t.Log(err)
	}
}

func removeMountFolders(t *testing.T, partitions []*types.Partition) {
	for _, p := range partitions {
		if p.MountPath != "" {
			removeFile(t, p.MountPath)
		}
	}
}

// returns true if no error, false if error
func runIgnition(t *testing.T, stage, root, configDir string, expectFail bool) bool {
	args := []string{"-clear-cache", "-oem", "file", "-stage", stage, "-root", root}
	cmd := exec.Command("ignition", args...)
	t.Log("ignition", args)
	cmd.Dir = configDir
	out, err := cmd.CombinedOutput()
	if err != nil && !expectFail {
		t.Fatal(args, err, string(out))
	}
	return err == nil
}

// pickDevice will return the device corresponding to a partition with a given
// label in the given imageFile
func pickDevice(t *testing.T, partitions []*types.Partition, fileName string, label string) string {
	number := -1
	for _, p := range partitions {
		if p.Label == label {
			number = p.Number
		}
	}
	if number == -1 {
		return ""
	}

	args := []string{"-l", fileName}
	kpartxOut := mustRun(t, "kpartx", args...)
	re := regexp.MustCompile("/dev/(?P<device>[\\w\\d]+)")
	match := re.FindSubmatch(kpartxOut)
	if len(match) < 2 {
		t.Log(string(kpartxOut))
		t.Fatal("couldn't find device")
	}
	return fmt.Sprintf("/dev/mapper/%sp%d", string(match[1]), number)
}

func writeIgnitionConfig(t *testing.T, config string) string {
	tmpDir, err := ioutil.TempDir("", "config")
	if err != nil {
		t.Fatal(err)
	}
	err = ioutil.WriteFile(
		filepath.Join(tmpDir, "config.ign"), []byte(config), 0644)
	if err != nil {
		t.Fatal(err)
	}
	return tmpDir
}

func calculateImageSize(partitions []*types.Partition) int64 {
	// 63 is the number of sectors cgpt uses when generating a hybrid MBR
	size := int64(63 * 512)
	for _, p := range partitions {
		size += int64(align(p.Length, 512) * 512)
	}
	size = size + int64(4096*512) // extra room to allow for alignments
	return size
}

// createVolume will create the image file of the specified size, create a
// partition table in it, and generate mount paths for every partition
func createVolume(t *testing.T, imageFile string, size int64, cylinders int, heads int, sectorsPerTrack int, partitions []*types.Partition) {
	// attempt to create the file, will leave already existing files alone.
	// os.Truncate requires the file to already exist
	out, err := os.Create(imageFile)
	if err != nil {
		t.Fatal("create", err, out)
	}
	out.Close()

	// Truncate the file to the given size
	err = os.Truncate(imageFile, size)
	if err != nil {
		t.Fatal("truncate", err)
	}

	createPartitionTable(t, imageFile, partitions)

	for counter, partition := range partitions {
		if partition.TypeCode == "blank" || partition.FilesystemType == "" {
			continue
		}

		mntPath, err := ioutil.TempDir("", fmt.Sprintf("hd1p%d", counter))
		if err != nil {
			t.Fatal(err)
		}
		partition.MountPath = mntPath
	}
}

// setDevices will create devices for each of the partitions in the imageFile,
// and then will format each partition according to what's descrived in the
// partitions argument.
func setDevices(t *testing.T, imageFile string, partitions []*types.Partition) string {
	loopDevice := kpartxAdd(t, imageFile)

	for _, partition := range partitions {
		if partition.TypeCode == "blank" || partition.FilesystemType == "" {
			continue
		}

		partition.Device = fmt.Sprintf(
			"/dev/mapper/%sp%d", loopDevice, partition.Number)
		formatPartition(t, partition)
	}
	return fmt.Sprintf("/dev/%s", loopDevice)
}

func destroyDevices(t *testing.T, imageFile string, partitions []*types.Partition) {
	args := []string{"-d", imageFile}
	_ = mustRun(t, "kpartx", args...)

	/*
		// add additional cleanup as kpartx -d isn't removing all device mappers
		for _, partition := range partitions {
			args := []string{"remove", partition.Device}
			_ = mustRun(t, "dmsetup", args...)
		}
	*/
}
func formatPartition(t *testing.T, partition *types.Partition) {
	var mkfs string
	var opts, label, uuid []string

	switch partition.FilesystemType {
	case "vfat":
		mkfs = "mkfs.vfat"
		label = []string{"-n", partition.FilesystemLabel}
		uuid = []string{"-i", partition.FilesystemUUID}
	case "ext2", "ext4":
		mkfs = "mke2fs"
		opts = []string{
			"-t", partition.FilesystemType, "-b", "4096",
			"-i", "4096", "-I", "128", "-e", "remount-ro",
		}
		label = []string{"-L", partition.FilesystemLabel}
		uuid = []string{"-U", partition.FilesystemUUID}
	case "btrfs":
		mkfs = "mkfs.btrfs"
		label = []string{"--label", partition.FilesystemLabel}
		uuid = []string{"--uuid", partition.FilesystemUUID}
	case "xfs":
		mkfs = "mkfs.xfs"
		label = []string{"-L", partition.FilesystemLabel}
		uuid = []string{"-m", "uuid=" + partition.FilesystemUUID}
	case "swap":
		mkfs = "mkswap"
		label = []string{"-L", partition.FilesystemLabel}
		uuid = []string{"-U", partition.FilesystemUUID}
	default:
		if partition.FilesystemType == "blank" ||
			partition.FilesystemType == "" {
			return
		}
		t.Fatal("Unknown partition", partition.FilesystemType)
	}

	if partition.FilesystemLabel != "" {
		opts = append(opts, label...)
	}
	if partition.FilesystemUUID != "" {
		opts = append(opts, uuid...)
	}
	opts = append(opts, partition.Device)

	_ = mustRun(t, mkfs, opts...)

	if (partition.FilesystemType == "ext2" || partition.FilesystemType == "ext4") && partition.TypeCode == "coreos-usr" {
		// this is done to mirror the functionality from disk_util
		opts := []string{
			"-U", "clear", "-T", "20091119110000", "-c", "0", "-i", "0",
			"-m", "0", "-r", "0", "-e", "remount-ro", partition.Device,
		}
		_ = mustRun(t, "tune2fs", opts...)
	}
}

func align(count int, alignment int) int {
	offset := count % alignment
	if offset != 0 {
		count += alignment - offset
	}
	return count
}

func setOffsets(partitions []*types.Partition) {
	// 34 is the first non-reserved GPT sector
	offset := 34
	for _, p := range partitions {
		if p.Length == 0 || p.TypeCode == "blank" {
			continue
		}
		offset = align(offset, 4096)
		p.Offset = offset
		offset += p.Length
	}
}

func createPartitionTable(t *testing.T, imageFile string, partitions []*types.Partition) {
	opts := []string{imageFile}
	hybrids := []int{}
	for _, p := range partitions {
		if p.TypeCode == "blank" || p.Length == 0 {
			continue
		}
		opts = append(opts, fmt.Sprintf(
			"--new=%d:%d:+%d", p.Number, p.Offset, p.Length))
		opts = append(opts, fmt.Sprintf(
			"--change-name=%d:%s", p.Number, p.Label))
		if p.TypeGUID != "" {
			opts = append(opts, fmt.Sprintf(
				"--typecode=%d:%s", p.Number, p.TypeGUID))
		}
		if p.GUID != "" {
			opts = append(opts, fmt.Sprintf(
				"--partition-guid=%d:%s", p.Number, p.GUID))
		}
		if p.Hybrid {
			hybrids = append(hybrids, p.Number)
		}
	}
	if len(hybrids) > 0 {
		if len(hybrids) > 3 {
			t.Fatal("Can't have more than three hybrids")
		} else {
			opts = append(opts, fmt.Sprintf("-h=%s", intJoin(hybrids, ":")))
		}
	}
	_ = mustRun(t, "sgdisk", opts...)
}

// kpartxAdd will use kpartx to add partition mappings for all of the partitions
// contained in the imageFile. This creates devices such as /dev/mapper/loop1p1
// corresponding to partitions in the imageFile. The loop device name (e.g.
// "loop1") will be returned.
func kpartxAdd(t *testing.T, imageFile string) string {
	args := []string{"-avs", imageFile}
	_ = mustRun(t, "kpartx", args...)
	args = []string{"-l", imageFile}
	kpartxOut := mustRun(t, "kpartx", args...)
	re := regexp.MustCompile("/dev/(?P<device>[\\w\\d]+)")
	match := re.FindSubmatch(kpartxOut)
	if len(match) < 2 {
		t.Log(string(kpartxOut))
		t.Fatal("couldn't find device")
	}
	return string(match[1])
}

func mountRootPartition(t *testing.T, partitions []*types.Partition) bool {
	for _, partition := range partitions {
		if partition.Label != "ROOT" {
			continue
		}
		args := []string{partition.Device, partition.MountPath}
		_ = mustRun(t, "mount", args...)
		return true
	}
	return false
}

func mountPartitions(t *testing.T, partitions []*types.Partition) {
	for _, partition := range partitions {
		if partition.FilesystemType == "" || partition.FilesystemType == "swap" || partition.Label == "ROOT" {
			continue
		}
		args := []string{partition.Device, partition.MountPath}
		_ = mustRun(t, "mount", args...)
	}
}

func updateTypeGUID(t *testing.T, partition *types.Partition) {
	partitionTypes := map[string]string{
		"coreos-resize":   "3884DD41-8582-4404-B9A8-E9B84F2DF50E",
		"data":            "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
		"coreos-rootfs":   "5DFBF5F4-2848-4BAC-AA5E-0D9A20B745A6",
		"bios":            "21686148-6449-6E6F-744E-656564454649",
		"efi":             "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
		"coreos-reserved": "C95DC21A-DF0E-4340-8D7B-26CBFA9A03E0",
	}

	if partition.TypeCode == "" || partition.TypeCode == "blank" {
		return
	}

	partition.TypeGUID = partitionTypes[partition.TypeCode]
	if partition.TypeGUID == "" {
		t.Fatal("Unknown TypeCode", partition.TypeCode)
	}
}

func intJoin(ints []int, delimiter string) string {
	strArr := []string{}
	for _, i := range ints {
		strArr = append(strArr, strconv.Itoa(i))
	}
	return strings.Join(strArr, delimiter)
}

func removeEmpty(strings []string) []string {
	var r []string
	for _, str := range strings {
		if str != "" {
			r = append(r, str)
		}
	}
	return r
}

func generateUUID(t *testing.T) string {
	out := mustRun(t, "uuidgen")
	return strings.TrimSpace(string(out))
}

func createFiles(t *testing.T, partitions []*types.Partition) {
	for _, partition := range partitions {
		if partition.Files == nil {
			continue
		}
		for _, file := range partition.Files {
			err := os.MkdirAll(filepath.Join(
				partition.MountPath, file.Directory), 0755)
			if err != nil {
				t.Fatal("mkdirall", err)
			}
			f, err := os.Create(filepath.Join(
				partition.MountPath, file.Directory, file.Name))
			if err != nil {
				t.Fatal("create", err, f)
			}
			defer f.Close()
			if file.Contents != "" {
				writer := bufio.NewWriter(f)
				writeStringOut, err := writer.WriteString(file.Contents)
				if err != nil {
					t.Fatal("writeString", err, string(writeStringOut))
				}
				writer.Flush()
			}
		}
	}
}

func unmountRootPartition(t *testing.T, partitions []*types.Partition) {
	for _, partition := range partitions {
		if partition.Label != "ROOT" {
			continue
		}

		_ = mustRun(t, "umount", partition.Device)
	}
}

func unmountPartitions(t *testing.T, partitions []*types.Partition) {
	for _, partition := range partitions {
		if partition.FilesystemType == "" || partition.FilesystemType == "swap" || partition.Label == "ROOT" {
			continue
		}

		_ = mustRun(t, "umount", partition.Device)
	}
}

func setExpectedPartitionsDrive(actual []*types.Partition, expected []*types.Partition) {
	for _, a := range actual {
		for _, e := range expected {
			if a.Number == e.Number {
				e.MountPath = a.MountPath
				e.Device = a.Device
				break
			}
		}
	}
}
