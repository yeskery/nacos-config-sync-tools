package credentials

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

var (
	advapi32        = windows.NewLazySystemDLL("Advapi32.dll")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

type WindowsStore struct{}

func NewStore() Store {
	return WindowsStore{}
}

func (WindowsStore) Save(target, username, password string) (string, error) {
	if target == "" || username == "" {
		return "", errors.New("credential target and username are required")
	}
	secret := windows.StringToUTF16(password)
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return "", err
	}
	userPtr, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return "", err
	}
	cred := credential{
		Type:               credTypeGeneric,
		TargetName:         targetPtr,
		CredentialBlobSize: uint32(len(secret) * 2),
		CredentialBlob:     (*byte)(unsafe.Pointer(&secret[0])),
		Persist:            credPersistLocalMachine,
		UserName:           userPtr,
	}
	r1, _, err := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r1 == 0 {
		return "", fmt.Errorf("write Windows credential: %w", err)
	}
	return MakeRef(target, username), nil
}

func (WindowsStore) Load(ref string) (string, error) {
	target, _, err := ParseRef(ref)
	if err != nil {
		return "", err
	}
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return "", err
	}
	var raw *credential
	r1, _, err := procCredReadW.Call(uintptr(unsafe.Pointer(targetPtr)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&raw)))
	if r1 == 0 {
		return "", fmt.Errorf("read Windows credential: %w", err)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(raw)))
	if raw == nil || raw.CredentialBlob == nil || raw.CredentialBlobSize == 0 {
		return "", nil
	}
	utf16Len := raw.CredentialBlobSize / 2
	data := unsafe.Slice((*uint16)(unsafe.Pointer(raw.CredentialBlob)), utf16Len)
	return windows.UTF16ToString(data), nil
}

func (WindowsStore) Delete(ref string) error {
	target, _, err := ParseRef(ref)
	if err != nil {
		return err
	}
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	r1, _, callErr := procCredDeleteW.Call(uintptr(unsafe.Pointer(targetPtr)), credTypeGeneric, 0)
	if r1 == 0 {
		return fmt.Errorf("delete Windows credential: %w", callErr)
	}
	return nil
}

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}
