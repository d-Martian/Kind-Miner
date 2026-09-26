//go:build !linux

package nodo

import "errors"

func watchFile(string) (<-chan struct{}, func(), error) {
	return nil, nil, errors.New("watching the Nodo config is only supported on Linux")
}
