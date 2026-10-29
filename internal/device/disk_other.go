//go:build !linux

package device

import "errors"

func diskUsage(path string) (*Disk, error) {
	return nil, errors.New("disk usage is only available on Linux")
}
