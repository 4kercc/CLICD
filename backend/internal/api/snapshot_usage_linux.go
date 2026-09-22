//go:build linux

package api

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unsafe"

	"clicd/internal/config"

	"golang.org/x/sys/unix"
)

// FS_IOC_FIEMAP = _IOWR('f', 11, struct fiemap): 0xC0000000 | (32<<16) | ('f'<<8) | 11
const fsIOCFiemap = 0xC020660B

const (
	fiemapFlagSync    = 0x00000001
	fiemapExtentLast  = 0x00000001
	fiemapExtentShare = 0x00002000
)

type fiemapHeader struct {
	Start         uint64
	Length        uint64
	Flags         uint32
	MappedExtents uint32
	ExtentCount   uint32
	Reserved      uint32
}

type fiemapExtent struct {
	Logical    uint64
	Physical   uint64
	Length     uint64
	Reserved64 [2]uint64
	Flags      uint32
	Reserved   [3]uint32
}

// fileAllocatedAndSharedBytes walks one file's extent map through FIEMAP.
// "shared" is the portion the filesystem reports as referenced by more than one
// file (a reflink clone), i.e. the bytes that would survive deleting this file.
func fileAllocatedAndSharedBytes(path string) (int64, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	fd := int(file.Fd())

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return 0, 0, err
	}
	allocated := int64(stat.Blocks) * 512

	const batch = 256
	headerSize := int(unsafe.Sizeof(fiemapHeader{}))
	extentSize := int(unsafe.Sizeof(fiemapExtent{}))
	buf := make([]byte, headerSize+batch*extentSize)
	header := (*fiemapHeader)(unsafe.Pointer(&buf[0]))
	extents := unsafe.Slice((*fiemapExtent)(unsafe.Pointer(&buf[headerSize])), batch)

	var shared int64
	var start uint64
	for {
		*header = fiemapHeader{
			Start:        start,
			Length:       ^uint64(0),
			Flags:        fiemapFlagSync,
			ExtentCount:  batch,
		}
		if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(fsIOCFiemap), uintptr(unsafe.Pointer(&buf[0]))); errno != 0 {
			return 0, 0, errno
		}
		mapped := int(header.MappedExtents)
		if mapped > batch {
			mapped = batch
		}
		if mapped == 0 {
			break
		}
		last := extents[mapped-1]
		for i := 0; i < mapped; i++ {
			if extents[i].Flags&fiemapExtentShare != 0 {
				shared += int64(extents[i].Length)
			}
		}
		if mapped < batch || last.Flags&fiemapExtentLast != 0 {
			break
		}
		start = last.Logical + last.Length
	}
	return allocated, shared, nil
}

// Extent maps change while the guest keeps writing, so cached measurements go
// stale quickly; a short TTL keeps list requests from rescanning every time.
const snapshotUniqueTTL = 20 * time.Second

type snapshotUniqueEntry struct {
	unique int64
	at     time.Time
}

var (
	snapshotUniqueMu    sync.Mutex
	snapshotUniqueCache = map[string]snapshotUniqueEntry{}
)

// snapshotUniqueBytes reports how much space deleting this snapshot would free:
// the part of it no other file references. On a reflink-capable filesystem a
// freshly taken snapshot shares almost everything with the running disk, so this
// is far smaller than SizeBytes and grows as the guest overwrites shared blocks.
//
// nil means "not measurable here": LXC snapshots are rootfs trees rather than a
// single clone-able image, and filesystems without extent maps report nothing.
func snapshotUniqueBytes(snapshot *config.Snapshot) *int64 {
	if snapshot == nil || snapshot.Path == "" || snapshot.SizeBytes <= 0 {
		return nil
	}
	diskPath := filepath.Join(snapshot.Path, "disk.qcow2")
	if _, err := os.Stat(diskPath); err != nil {
		return nil
	}

	key := snapshot.ID + ":" + strconv.FormatInt(snapshot.SizeBytes, 10)
	snapshotUniqueMu.Lock()
	if entry, ok := snapshotUniqueCache[key]; ok && time.Since(entry.at) < snapshotUniqueTTL {
		snapshotUniqueMu.Unlock()
		unique := entry.unique
		return &unique
	}
	snapshotUniqueMu.Unlock()

	_, shared, err := fileAllocatedAndSharedBytes(diskPath)
	if err != nil {
		return nil
	}
	unique := snapshot.SizeBytes - shared
	if unique < 0 {
		unique = 0
	}

	snapshotUniqueMu.Lock()
	if len(snapshotUniqueCache) > 256 {
		snapshotUniqueCache = map[string]snapshotUniqueEntry{}
	}
	snapshotUniqueCache[key] = snapshotUniqueEntry{unique: unique, at: time.Now()}
	snapshotUniqueMu.Unlock()

	return &unique
}

func decorateSnapshotUsage(snapshots []config.Snapshot) []config.Snapshot {
	for i := range snapshots {
		snapshots[i].UniqueBytes = snapshotUniqueBytes(&snapshots[i])
	}
	return snapshots
}
