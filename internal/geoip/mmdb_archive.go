package geoip

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

const maximumArchiveEntries = 32

const maximumCompressionRatio = 100

func extractDatabase(archive, directory string) (string, error) {
	file, err := os.Open(archive)
	if err != nil {
		return "", ErrUnavailable
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", ErrUnavailable
	}
	buffered := bufio.NewReader(file)
	zipped, err := gzip.NewReader(buffered)
	if err != nil {
		return "", ErrInvalid
	}
	defer func() { _ = zipped.Close() }()
	zipped.Multistream(false)
	limited := &io.LimitedReader{R: zipped, N: MaximumMMDBBytes + 1}
	reader := tar.NewReader(limited)
	state := archiveState{seen: map[string]bool{}, maximum: info.Size() * maximumCompressionRatio, directory: directory}
	result, err := state.extract(reader)
	if err == nil {
		err = finishArchiveRead(limited, buffered)
	}
	if err != nil && state.database != "" {
		_ = os.Remove(state.database)
	}
	return result, err
}

type archiveState struct {
	seen                map[string]bool
	maximum, total      int64
	directory, database string
	count               int
}

func (state *archiveState) extract(reader *tar.Reader) (string, error) {
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", ErrInvalid
		}
		state.count++
		if err = state.validateEntry(header); err != nil {
			return "", err
		}
		if err = state.consumeEntry(reader, header); err != nil {
			return "", err
		}
	}
	if state.database == "" {
		return "", ErrInvalid
	}
	return state.database, nil
}

func (state *archiveState) validateEntry(header *tar.Header) error {
	name := strings.TrimSuffix(header.Name, "/")
	if !validArchiveName(name) || state.seen[name] {
		return ErrInvalid
	}
	state.seen[name] = true
	if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir {
		return ErrInvalid
	}
	if header.Size < 0 || header.Size > MaximumMMDBBytes || state.count > maximumArchiveEntries {
		return ErrInvalid
	}
	state.total += header.Size
	if state.total > MaximumMMDBBytes || state.total > state.maximum {
		return ErrInvalid
	}
	return nil
}

func writeArchiveDatabase(directory string, reader io.Reader, size int64) (string, error) {
	file, err := os.CreateTemp(directory, ".mmdb-extract-")
	if err != nil {
		return "", ErrUnavailable
	}
	name := file.Name()
	written, copyErr := io.Copy(file, io.LimitReader(reader, size+1))
	err = errors.Join(copyErr, file.Sync(), file.Close())
	if err != nil || written != size {
		_ = os.Remove(name)
		return "", ErrInvalid
	}
	return name, nil
}

func finishArchiveRead(limited *io.LimitedReader, buffered *bufio.Reader) error {
	if _, err := io.Copy(io.Discard, limited); err != nil || limited.N <= 0 {
		return ErrInvalid
	}
	if _, err := buffered.Peek(1); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}

func (state *archiveState) consumeEntry(reader *tar.Reader, header *tar.Header) error {
	if header.Typeflag == tar.TypeDir {
		return nil
	}
	if strings.HasSuffix(strings.ToLower(header.Name), ".mmdb") {
		if state.database != "" {
			return ErrInvalid
		}
		var err error
		state.database, err = writeArchiveDatabase(state.directory, reader, header.Size)
		return err
	}
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return ErrInvalid
	}
	return nil
}

func validArchiveName(name string) bool {
	return name != "" && len(name) <= 512 && !path.IsAbs(name) && path.Clean(name) == name && name != ".." && !strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\\x00")
}
