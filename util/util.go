package util

import (
	"encoding/hex"
	"log"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/deroproject/derosuite/address"
	"github.com/deroproject/derosuite/astrobwt"
	"github.com/deroproject/derosuite/blockchain"
	"github.com/deroproject/derosuite/crypto"
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

	//if len(addy) != len(poolAddy) {
	//	return false
	//}
	prefix, _ := utf8.DecodeRuneInString(addy)
	poolPrefix, _ := utf8.DecodeRuneInString(poolAddy)
	if prefix != poolPrefix {
		return false
	}
	addyRune := []rune(addy)
	poolAddyRune := []rune(poolAddy)
	// Validating only first 3 (dET or dER) since possibly integrated addrs could be dETi or dERi and pool addr could be either dETi, dERi, dETo, dERo [i for integrated]
	poolAddyNetwork = string(poolAddyRune[0:2])

	if string(addyRune[0:2]) != poolAddyNetwork {
		log.Printf("Invalid address, pool address and supplied address don't match testnet(dETo)/mainnet(dERo). Pool Address is in %s", poolAddyNetwork)
		return false
	}

	// Call NewAddress to confirm address validation from "github.com/deroproject/derosuite/address"
	_, err := address.NewAddress(strings.TrimSpace(addy))
	if err != nil {
		log.Printf("Address validation failed for '%s': %s", addy, err)
		return false
	}

	return true
}

func reverse(src []byte) []byte {
	dst := make([]byte, len(src))
	for i := len(src); i > 0; i-- {
		dst[len(src)-i] = src[i-1]
	}
	return dst
}

func AstroBWTHash(shareBuff []byte, diff big.Int) (bool, bool) {
	//checkPowHashBig := false
	var powhash crypto.Hash
	var data astrobwt.Data
	var max_pow_size int = 819200 //astrobwt.MAX_LENGTH

	//hashBytes, _ = hex.DecodeString(result)

	hash, success := astrobwt.POW_optimized_v2(shareBuff, max_pow_size, &data)
	if !success || hash[len(hash)-1] != 0 {
		//fmt.Printf("[IncorrectPoW-171] %+v\n", shareBuff)
		//fmt.Printf("[IncorrectPoW-172] %+v\n", hash)
		/*minerOutput := "Incorrect PoW - if you see often, check input on miner software"
		log.Printf("Bad hash from miner (l174) %v@%v", m.id, cs.ip)

		if shareType == "Trusted" {
			log.Printf("[No Trust] Miner is no longer submitting trusted shares: %v@%v", m.id, cs.ip)
			shareType = "Valid"
		}*/

		return false, false

		//atomic.AddInt64(&m.invalidShares, 1)
		//atomic.StoreInt64(&m.trustedShares, 0)
		//return false, minerOutput
	}

	//atomic.AddInt64(&m.trustedShares, 1)

	//fmt.Printf("[%s Share] %+v\n", shareType, hashBytes)

	copy(powhash[:], hash[:])

	checkPowHashBig := blockchain.CheckPowHashBig(powhash, &diff)

	return checkPowHashBig, success
}
