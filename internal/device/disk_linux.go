//go:build linux

package device

import "syscall"

func diskUsage(path string) (*Disk, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return nil, err
	}
	bs := uint64(st.Bsize)
	return &Disk{
		Path:       path,
		TotalBytes: st.Blocks * bs,
		FreeBytes:  st.Bavail * bs,
		UsedBytes:  (st.Blocks - st.Bfree) * bs,
	}, nil
}
