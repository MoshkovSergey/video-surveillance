//go:build windows

package recorder

import (
	"os"
	"syscall"
	"time"
)

// fileCreationTime возвращает время создания файла на диске Windows.
// Для сегментов MediaMTX это момент первого записанного ключевого кадра,
// то есть истинное начало содержимого файла.
func fileCreationTime(path string) (time.Time, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	sys, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(0, sys.CreationTime.Nanoseconds()), true
}