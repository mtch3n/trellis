//go:build windows

package daemon

import (
	"crypto/rand"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func Endpoint(root string) string { return filepath.Join(root, "daemon.endpoint") }

func Listen(root string) (net.Listener, func(), error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	token := rand.Text()
	b, err := json.Marshal(map[string]string{"address": l.Addr().String(), "token": token})
	if err == nil {
		err = writePrivateEndpoint(Endpoint(root), b)
	}
	if err != nil {
		l.Close()
		return nil, nil, err
	}
	return &authenticatedListener{Listener: l, token: token}, func() { _ = l.Close(); _ = os.Remove(Endpoint(root)) }, nil
}

// An explicit DACL is necessary on Windows: Unix mode 0600 alone does not
// restrict access to the bearer credential in an inherited public directory.
func writePrivateEndpoint(path string, data []byte) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	attrs := windows.SecurityAttributes{SecurityDescriptor: sd}
	attrs.Length = uint32(unsafe.Sizeof(attrs))
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, windows.FILE_SHARE_READ, &attrs, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(handle), path)
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	return closeErr
}
