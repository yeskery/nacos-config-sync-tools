//go:build !windows

package credentials

func NewStore() Store {
	return NewFileStore(defaultCredentialsPath())
}
