//go:build windows

package core

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	fsctlSetReparsePoint    = 0x000900A4
	ioReparseTagMountPoint  = 0xA0000003
	mountPointHeaderLen     = 8 // SubstituteNameOffset/Length + PrintNameOffset/Length
	reparseDataHeaderLen    = 8 // ReparseTag + ReparseDataLength + Reserved
	fileFlagOpenReparsePt   = 0x00200000
	fileFlagBackupSemantics = 0x02000000
)

// Link points link at target using an NTFS directory junction on Windows,
// which requires no administrator privileges.
func Link(target, link string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("couldn't create junction %s: %w", link, err)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return fmt.Errorf("couldn't create junction %s: %w", link, err)
	}
	if err := os.Mkdir(link, 0o755); err != nil {
		return fmt.Errorf("couldn't create junction %s: %w", link, err)
	}
	if err := setJunctionReparsePoint(link, absTarget); err != nil {
		_ = os.Remove(link)
		return fmt.Errorf("couldn't create junction %s: %w", link, err)
	}
	return nil
}

func setJunctionReparsePoint(link, absTarget string) error {
	linkPtr, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		linkPtr,
		windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		fileFlagOpenReparsePt|fileFlagBackupSemantics,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	cleanTarget := strings.TrimPrefix(absTarget, `\\?\`)
	substitute := `\??\` + cleanTarget
	subU16 := utf16.Encode([]rune(substitute))
	printU16 := utf16.Encode([]rune(cleanTarget))

	subByteLen := len(subU16) * 2
	printByteLen := len(printU16) * 2
	// Buffer layout: 8-byte reparse header + 8-byte mount point header +
	// substitute UTF-16 + NUL + print UTF-16 + NUL.
	dataLen := mountPointHeaderLen + subByteLen + 2 + printByteLen + 2
	buf := make([]byte, reparseDataHeaderLen+dataLen)

	binary.LittleEndian.PutUint32(buf[0:4], ioReparseTagMountPoint)
	binary.LittleEndian.PutUint16(buf[4:6], uint16(dataLen))
	binary.LittleEndian.PutUint16(buf[6:8], 0)

	// MountPointReparseBuffer offsets are relative to PathBuffer.
	binary.LittleEndian.PutUint16(buf[8:10], 0)
	binary.LittleEndian.PutUint16(buf[10:12], uint16(subByteLen))
	binary.LittleEndian.PutUint16(buf[12:14], uint16(subByteLen+2))
	binary.LittleEndian.PutUint16(buf[14:16], uint16(printByteLen))

	off := 16
	for _, u := range subU16 {
		binary.LittleEndian.PutUint16(buf[off:off+2], u)
		off += 2
	}
	off += 2 // NUL terminator
	for _, u := range printU16 {
		binary.LittleEndian.PutUint16(buf[off:off+2], u)
		off += 2
	}

	var returned uint32
	return windows.DeviceIoControl(
		handle,
		fsctlSetReparsePoint,
		(*byte)(unsafe.Pointer(&buf[0])),
		uint32(len(buf)),
		nil,
		0,
		&returned,
		nil,
	)
}

// Unlink removes a symlink or directory junction on Windows without deleting
// the target's contents.
func Unlink(p string) error {
	if err := os.Remove(p); err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	return nil
}
