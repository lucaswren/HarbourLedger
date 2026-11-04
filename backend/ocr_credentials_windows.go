package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsOCRCredentials struct{ target string }
type windowsCredential struct {
	flags, kind             uint32
	target, comment         *uint16
	lastWritten             windows.Filetime
	size                    uint32
	blob                    *byte
	persist, attributeCount uint32
	attributes              uintptr
	alias, username         *uint16
}

var credentialAPI = windows.NewLazySystemDLL("advapi32.dll")

func localOCRCredentials() ocrCredentialStore {
	return windowsOCRCredentials{target: "Harbour Ledger / Tencent OCR"}
}

func (c windowsOCRCredentials) read() (map[string]string, error) {
	var item *windowsCredential
	target := windows.StringToUTF16Ptr(c.target)
	ok, _, err := credentialAPI.NewProc("CredReadW").Call(uintptr(unsafe.Pointer(target)), 1, 0, uintptr(unsafe.Pointer(&item)))
	if ok == 0 {
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			return nil, nil
		}
		return nil, fmt.Errorf("無法讀取 Windows OCR 憑據：%w", err)
	}
	defer credentialAPI.NewProc("CredFree").Call(uintptr(unsafe.Pointer(item)))
	var values map[string]string
	if json.Unmarshal(unsafe.Slice(item.blob, item.size), &values) != nil || values == nil {
		return nil, fmt.Errorf("Windows OCR 憑據格式無效")
	}
	return values, nil
}

func (c windowsOCRCredentials) write(values map[string]string) error {
	target := windows.StringToUTF16Ptr(c.target)
	if len(values) == 0 {
		ok, _, err := credentialAPI.NewProc("CredDeleteW").Call(uintptr(unsafe.Pointer(target)), 1, 0)
		if ok == 0 && !errors.Is(err, windows.ERROR_NOT_FOUND) {
			return fmt.Errorf("無法清除 Windows OCR 憑據：%w", err)
		}
		return nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return err
	}
	// CRED_TYPE_GENERIC with CRED_PERSIST_LOCAL_MACHINE is scoped to this user on this device.
	if len(raw) > 2560 {
		return fmt.Errorf("OCR 憑據超過 Windows 憑據管理員的容量")
	}
	item := windowsCredential{kind: 1, target: target, size: uint32(len(raw)), blob: &raw[0], persist: 2}
	ok, _, err := credentialAPI.NewProc("CredWriteW").Call(uintptr(unsafe.Pointer(&item)), 0)
	if ok == 0 {
		return fmt.Errorf("無法保存 Windows OCR 憑據：%w", err)
	}
	return nil
}
