package ipc

import "errors"

const localPipePrefix = `\\.\pipe\`

// PipePath expands a logical pipe name into the local Windows pipe namespace.
// Callers cannot provide a UNC host or nested namespace.
func PipePath(name string) (string, error) {
	if name == "" || len(name) > 128 {
		return "", errors.New("companion pipe name is invalid")
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return "", errors.New("companion pipe name is invalid")
	}
	return localPipePrefix + name, nil
}
