package astrobwtutil

import "github.com/deroproject/derosuite/globals"

var wallet_address string

func ValidateAddress(addr string) bool {
	a, err := globals.ParseValidateAddress(addr)
	if err != nil {
		return false
	}
	wallet_address = a.String()
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
