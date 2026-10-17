package check

import (
	"encoding/binary"
	"fmt"
)

// WavProblem says why data is not a sound the device plays (16 kHz, mono, 16-bit PCM WAV within
// the size limit), or "".
func WavProblem(data []byte) string {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return "not a WAV file"
	}
	formatOK := false
	for at := 12; at+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[at+4:]))
		body := at + 8
		if size < 0 || size > len(data)-body {
			return "a chunk runs past the end of the file"
		}
		switch string(data[at : at+4]) {
		case "fmt ":
			if size < 16 {
				return "the format chunk is too short"
			}
			f := data[body:]
			if binary.LittleEndian.Uint16(f[0:]) != 1 || binary.LittleEndian.Uint16(f[2:]) != 1 ||
				int(binary.LittleEndian.Uint32(f[4:])) != registryLimits.SampleRate || binary.LittleEndian.Uint16(f[14:]) != 16 {
				return fmt.Sprintf("must be PCM, mono, %d Hz, 16-bit", registryLimits.SampleRate)
			}
			formatOK = true
		case "data":
			switch {
			case !formatOK:
				return "the data chunk comes before the format chunk"
			case size == 0 || size%2 != 0:
				return "no samples"
			case size > registryLimits.SoundBytesMax:
				return fmt.Sprintf("%d bytes of samples; the device plays at most %d (%.0f s)", size, registryLimits.SoundBytesMax,
					float64(registryLimits.SoundBytesMax)/2/float64(registryLimits.SampleRate))
			}
			return ""
		}
		at = body + size + size%2
	}
	return "no data chunk"
}
