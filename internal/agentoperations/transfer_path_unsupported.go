//go:build !linux && !windows

package agentoperations

import "os"

type transferRoot struct{}

type transferDestination struct{}

func openTransferRoot(string) (*transferRoot, error) { return nil, ErrBoundary }

func (*transferRoot) Close() error { return ErrBoundary }

func (*transferRoot) openSource(string) (*os.File, error) { return nil, ErrBoundary }

func (*transferRoot) destination(string) (*transferDestination, error) { return nil, ErrBoundary }

func (*transferDestination) Close() error { return ErrBoundary }

func (*transferDestination) temporary(string, bool) (*os.File, error) { return nil, ErrBoundary }

func (*transferDestination) current() (*os.File, error) { return nil, ErrBoundary }

func (*transferDestination) publish(string, bool) error { return ErrBoundary }

func (*transferDestination) removeTemporary(string) error { return ErrBoundary }

func transferFileIdentity(*os.File) (string, error) { return "", ErrBoundary }

func transferFileVersion(*os.File) (string, error) { return "", ErrBoundary }

func setTransferMode(*os.File, uint32) error { return ErrBoundary }

func nativeNotExist(error) bool { return false }

func transferModeMatches(*os.File, uint32) bool { return false }
