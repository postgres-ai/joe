/*
2019 © Postgres.ai
*/

package pgexplain

import (
	"fmt"
	"math"
)

// Based on dustin/go-humanize.
// https://github.com/dustin/go-humanize/blob/master/bytes.go
// The lib lacks an ability to change float number format.

// IEC Sizes.
// kibis of bits.
const (
	Byte = 1 << (iota * 10)
	KiByte
	MiByte
	GiByte
	TiByte
	PiByte
	EiByte
)

// SI Sizes.
const (
	IByte = 1
	KByte = IByte * 1000
	MByte = KByte * 1000
	GByte = MByte * 1000
	TByte = GByte * 1000
	PByte = TByte * 1000
	EByte = PByte * 1000
)

var bytesSizeTable = map[string]uint64{
	"b":   Byte,
	"kib": KiByte,
	"kb":  KByte,
	"mib": MiByte,
	"mb":  MByte,
	"gib": GiByte,
	"gb":  GByte,
	"tib": TiByte,
	"tb":  TByte,
	"pib": PiByte,
	"pb":  PByte,
	"eib": EiByte,
	"eb":  EByte,
	// Without suffix
	"":   Byte,
	"ki": KiByte,
	"k":  KByte,
	"mi": MiByte,
	"m":  MByte,
	"gi": GiByte,
	"g":  GByte,
	"ti": TiByte,
	"t":  TByte,
	"pi": PiByte,
	"p":  PByte,
	"ei": EiByte,
	"e":  EByte,
}

func logn(n, b float64) float64 {
	return math.Log(n) / math.Log(b)
}

func humanateBytes(s uint64, base float64, sizes []string, f string) string {
	if len(f) == 0 {
		f = "%.0f %s"
	}

	if s < 10 {
		return fmt.Sprintf("%d B", s)
	}
	e := math.Floor(logn(float64(s), base))
	suffix := sizes[int(e)]
	val := math.Floor(float64(s)/math.Pow(base, e)*10+0.5) / 10

	return fmt.Sprintf(f, val, suffix)
}

// Bytes produces a human readable representation of an SI size.
// Bytes(82854982) -> 83 MB
func Bytes(s uint64, f string) string {
	sizes := []string{"B", "kB", "MB", "GB", "TB", "PB", "EB"}
	return humanateBytes(s, 1000, sizes, f)
}

// IBytes produces a human readable representation of an IEC size.
// IBytes(82854982) -> 79 MiB
func IBytes(s uint64, f string) string {
	sizes := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	return humanateBytes(s, 1024, sizes, f)
}

func blocksToBytes(blocks uint64) string {
	bytes := blocks * 1024 * 8
	return IBytes(bytes, "%.02f %s")
}
