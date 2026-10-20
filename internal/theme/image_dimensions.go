package theme

import "encoding/binary"

func validateWebP(content []byte) error {
	if len(content) < 25 || uint64(binary.LittleEndian.Uint32(content[4:8]))+8 != uint64(len(content)) {
		return ErrArchiveInvalid
	}
	var width, height uint32
	switch string(content[12:16]) {
	case "VP8L":
		if content[20] != 0x2f {
			return ErrArchiveInvalid
		}
		packed := binary.LittleEndian.Uint32(content[21:25])
		width, height = (packed&0x3fff)+1, ((packed>>14)&0x3fff)+1
	case "VP8X":
		if len(content) < 30 {
			return ErrArchiveInvalid
		}
		width, height = little24(content[24:27])+1, little24(content[27:30])+1
	case "VP8 ":
		if len(content) < 30 || string(content[23:26]) != "\x9d\x01\x2a" {
			return ErrArchiveInvalid
		}
		width = uint32(binary.LittleEndian.Uint16(content[26:28]) & 0x3fff)
		height = uint32(binary.LittleEndian.Uint16(content[28:30]) & 0x3fff)
	default:
		return ErrArchiveInvalid
	}
	if width == 0 || height == 0 || uint64(width)*uint64(height) > maxImagePixels {
		return ErrArchiveInvalid
	}
	return nil
}

func little24(content []byte) uint32 {
	return uint32(content[0]) | uint32(content[1])<<8 | uint32(content[2])<<16
}
