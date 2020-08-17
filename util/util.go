package util

import (
	"encoding/hex"
	"log"
	"math/big"
	"time"
	"unicode/utf8"

	"git.dero.io/Nelbert442/dero-golang-pool/astrobwtutil"
)

var Diff1 = StringToBig("0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF")

func StringToBig(h string) *big.Int {
	n := new(big.Int)
	n.SetString(h, 0)
	return n
}

func MakeTimestamp() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func GetTargetHex(diff int64) string {
	padded := make([]byte, 32)

	diffBuff := new(big.Int).Div(Diff1, big.NewInt(diff)).Bytes()
	copy(padded[32-len(diffBuff):], diffBuff)
	buff := padded[0:4]
	targetHex := hex.EncodeToString(reverse(buff))
	return targetHex
}

func GetHashDifficulty(hashBytes []byte) (*big.Int, bool) {
	diff := new(big.Int)
	diff.SetBytes(reverse(hashBytes))

	// Check for broken result, empty string or zero hex value
	if diff.Cmp(new(big.Int)) == 0 {
		return nil, false
	}
	return diff.Div(Diff1, diff), true
}

func ValidateAddress(addy string, poolAddy string) bool {
	var poolAddyNetwork string

	if len(addy) != len(poolAddy) {
		return false
	}
	prefix, _ := utf8.DecodeRuneInString(addy)
	poolPrefix, _ := utf8.DecodeRuneInString(poolAddy)
	if prefix != poolPrefix {
		return false
	}
	addyRune := []rune(addy)
	poolAddyRune := []rune(poolAddy)
	poolAddyNetwork = string(poolAddyRune[0:4])

	if string(addyRune[0:4]) != poolAddyNetwork {
		log.Printf("Invalid address, pool address and supplied address don't match testnet(dETo)/mainnet(dERo). Pool Address is in %s", poolAddyNetwork)
		return false
	}
	return astrobwtutil.ValidateAddress(addy)
}

func reverse(src []byte) []byte {
	dst := make([]byte, len(src))
	for i := len(src); i > 0; i-- {
		dst[len(src)-i] = src[i-1]
	}
	return dst
}
