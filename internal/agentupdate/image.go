package agentupdate

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
)

type imageVerifier string

func (checker imageVerifier) verifyImage(ctx context.Context, file *os.File, image Image) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != image.Size {
		return ErrIntegrity
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, &contextReader{ctx: ctx, reader: file}); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != image.SHA256 {
		return ErrIntegrity
	}
	return verifyELF(file, image.Size, string(checker))
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}

func verifyELF(file io.ReaderAt, size int64, arch string) error {
	var header [64]byte
	if _, err := file.ReadAt(header[:], 0); err != nil {
		return ErrFormat
	}
	if !validELFHeader(header) {
		return ErrFormat
	}
	kind, machine := binary.LittleEndian.Uint16(header[16:18]), binary.LittleEndian.Uint16(header[18:20])
	if kind != 2 && kind != 3 {
		return ErrFormat
	}
	if !validELFMachine(arch, machine) {
		return ErrFormat
	}
	return verifyELFPrograms(file, size, header)
}

func verifyELFPrograms(file io.ReaderAt, size int64, header [64]byte) error {
	order := binary.LittleEndian
	entry, offset := order.Uint64(header[24:32]), order.Uint64(header[32:40])
	count, width := order.Uint16(header[56:58]), order.Uint16(header[54:56])
	if !validProgramTable(entry, programTable{count: count, width: width, offset: offset, size: uint64(size)}) {
		return ErrFormat
	}
	length := uint64(count) * uint64(width)
	if length > uint64(size)-offset {
		return ErrFormat
	}
	executable := false
	for index := uint64(0); index < uint64(count); index++ {
		var program [56]byte
		if _, err := file.ReadAt(program[:], int64(offset+index*56)); err != nil {
			return ErrFormat
		}
		contains, err := validELFProgram(program, uint64(size), entry)
		if err != nil {
			return err
		}
		executable = executable || contains
	}
	if !executable {
		return ErrFormat
	}
	return nil
}

func validELFProgram(program [56]byte, size, entry uint64) (bool, error) {
	order := binary.LittleEndian
	offset, address := order.Uint64(program[8:16]), order.Uint64(program[16:24])
	fileSize, memorySize := order.Uint64(program[32:40]), order.Uint64(program[40:48])
	if offset > size || fileSize > size-offset || memorySize < fileSize || address > ^uint64(0)-memorySize {
		return false, ErrFormat
	}
	load, execute := order.Uint32(program[:4]) == 1, order.Uint32(program[4:8])&1 != 0
	return load && execute && entry >= address && entry-address < memorySize, nil
}

func imageFor(value *journal) Image {
	return Image{Version: value.TargetVersion, Size: value.Assignment.Artifact.Size, SHA256: value.Assignment.Artifact.SHA256}
}

func validELFHeader(header [64]byte) bool {
	if string(header[:4]) != "\x7fELF" || header[4] != 2 || header[5] != 1 || header[6] != 1 {
		return false
	}
	if !validELFVersion(header) {
		return false
	}
	return true
}

func validELFVersion(header [64]byte) bool {
	return binary.LittleEndian.Uint32(header[20:24]) == 1 && binary.LittleEndian.Uint16(header[52:54]) == 64 && (header[7] == 0 || header[7] == 3)
}

type programTable struct {
	count, width uint16
	offset, size uint64
}

func validProgramTable(entry uint64, table programTable) bool {
	return entry != 0 && table.count > 0 && table.count <= 256 && table.width == 56 && table.offset <= table.size
}

func validELFMachine(arch string, machine uint16) bool {
	return (arch == "amd64" && machine == 62) || (arch == "arm64" && machine == 183)
}
