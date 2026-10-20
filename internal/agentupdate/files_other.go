//go:build !linux

package agentupdate

import "os"

func openDirectory(string) (*directory, error) { return nil, ErrManualOnly }

func openChildDirectory(*directory, string, bool) (*directory, error) { return nil, ErrManualOnly }

func (*directory) openRelative(string, int, os.FileMode) (*os.File, error) { return nil, ErrManualOnly }

func renameRelative(fileRef, fileRef) error { return ErrManualOnly }

func removeRelative(*directory, string) error { return ErrManualOnly }

func lockDirectory(*directory) (*os.File, error) { return nil, ErrManualOnly }

func unlockFile(*os.File) error { return ErrManualOnly }

func availableSpace(*directory) (uint64, error) { return 0, ErrManualOnly }
