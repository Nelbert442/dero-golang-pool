package astrobwtutil

import (
	"log"
	"strings"

	"github.com/deroproject/derosuite/address"
)

// Leverage NewAddress in derosuite: https://github.com/deroproject/derosuite/blob/master/address/address.go#L111
func ValidateAddress(str string) bool {
	_, err := address.NewAddress(strings.TrimSpace(str))
	if err != nil {
		log.Printf("Address validation failed for '%s': %s", str, err)
		return false
	}

	return true
}

func ConvertBlob(blob []byte) []byte {
	output := make([]byte, 76)
	//out := (*C.char)(unsafe.Pointer(&output[0]))

	//input := (*C.char)(unsafe.Pointer(&blob[0]))

	//size := (C.uint32_t)(len(blob))
	//C.convert_blob(input, size, out)
	return output
}
