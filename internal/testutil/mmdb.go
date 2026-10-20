package testutil

import "encoding/binary"

// CountryMMDB 返回完全合成的单节点国家库，所有地址使用同一测试国家；不包含真实GeoIP数据。
func CountryMMDB(country string, epoch uint32) []byte {
	data := []byte{0, 0, 17, 0, 0, 17}
	data = append(data, make([]byte, 16)...)
	data = append(data, 0xe1)
	data = append(data, mmdbString("country")...)
	data = append(data, 0xe1)
	data = append(data, mmdbString("iso_code")...)
	data = append(data, mmdbString(country)...)
	data = append(data, []byte("\xab\xcd\xefMaxMind.com")...)
	metadata := []byte{0xe9}
	metadata = append(metadata, mmdbString("node_count")...)
	metadata = append(metadata, 0xc1, 1)
	metadata = append(metadata, mmdbString("record_size")...)
	metadata = append(metadata, 0xa1, 24)
	metadata = append(metadata, mmdbString("ip_version")...)
	metadata = append(metadata, 0xa1, 4)
	metadata = append(metadata, mmdbString("database_type")...)
	metadata = append(metadata, mmdbString("GeoLite2-Country")...)
	metadata = append(metadata, mmdbString("languages")...)
	metadata = append(metadata, 1, 4)
	metadata = append(metadata, mmdbString("en")...)
	metadata = append(metadata, mmdbString("binary_format_major_version")...)
	metadata = append(metadata, 0xa1, 2)
	metadata = append(metadata, mmdbString("binary_format_minor_version")...)
	metadata = append(metadata, 0xa0)
	metadata = append(metadata, mmdbString("build_epoch")...)
	metadata = append(metadata, 0xc4)
	metadata = binary.BigEndian.AppendUint32(metadata, epoch)
	metadata = append(metadata, mmdbString("description")...)
	metadata = append(metadata, 0xe1)
	metadata = append(metadata, mmdbString("en")...)
	metadata = append(metadata, mmdbString("Synthetic fixture")...)
	return append(data, metadata...)
}

func mmdbString(value string) []byte {
	return append([]byte{0x40 | byte(len(value))}, []byte(value)...)
}
