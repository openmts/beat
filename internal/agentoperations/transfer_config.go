package agentoperations

func validateTransferRoots(config Config) error {
	for _, root := range config.FileRoots {
		if !root.Read && !root.Write {
			continue
		}
		if err := validateTransferRoot(root.Path); err != nil {
			return err
		}
	}
	return nil
}

func validateTransferRoot(path string) error {
	root, err := openTransferRoot(path)
	if err != nil {
		return err
	}
	return root.Close()
}
