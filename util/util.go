package util

import (
	"encoding/hex"
	"log"
	"math/big"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/deroproject/derosuite/address"
	"github.com/deroproject/derosuite/astrobwt"
	"github.com/deroproject/derosuite/blockchain"
	"github.com/deroproject/derosuite/crypto"
	"github.com/deroproject/derosuite/cryptonight"
)

var Diff1 = StringToBig("0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF")

var UtilInfoLogger = logFileOutUtil("INFO")
var UtilErrorLogger = logFileOutUtil("ERROR")

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

func ValidateAddressNonDERO(addy string, poolAddy string) bool {
	prefix, _ := utf8.DecodeRuneInString(addy)
	poolPrefix, _ := utf8.DecodeRuneInString(poolAddy)
	if prefix != poolPrefix {
		log.Printf("[Util] Address prefix (%v) and pool address prefix (%v) do not match. Invalid address.", prefix, poolPrefix)
		UtilErrorLogger.Printf("[Util] Address prefix (%v) and pool address prefix (%v) do not match. Invalid address.", prefix, poolPrefix)
		return false
	}
	addyRune := []rune(addy)
	poolAddyRune := []rune(poolAddy)
	// Validating only first 2 since usually they match in other coins. Could // TODO in future to properly handle or just skip this portion alltogether
	poolAddyNetwork := string(poolAddyRune[0:2])

	if string(addyRune[0:2]) != poolAddyNetwork {
		log.Printf("[Util] Invalid address, pool address and supplied address don't match.")
		UtilErrorLogger.Printf("[Util] Invalid address, pool address and supplied address don't match.")
		return false
	}

	return true
}

func ValidateAddress(addy string, poolAddy string) bool {
	prefix, _ := utf8.DecodeRuneInString(addy)
	poolPrefix, _ := utf8.DecodeRuneInString(poolAddy)
	if prefix != poolPrefix {
		log.Printf("[Util] Address prefix (%v) and pool address prefix (%v) do not match. Invalid address.", prefix, poolPrefix)
		UtilErrorLogger.Printf("[Util] Address prefix (%v) and pool address prefix (%v) do not match. Invalid address.", prefix, poolPrefix)
		return false
	}
	addyRune := []rune(addy)
	poolAddyRune := []rune(poolAddy)
	// Validating only first 3 (dET or dER) since possibly integrated addrs could be dETi or dERi and pool addr could be either dETi, dERi, dETo, dERo [i for integrated]
	poolAddyNetwork := string(poolAddyRune[0:3])

	if string(addyRune[0:3]) != poolAddyNetwork {
		log.Printf("[Util] Invalid address, pool address and supplied address don't match testnet(dETo)/mainnet(dERo).")
		UtilErrorLogger.Printf("[Util] Invalid address, pool address and supplied address don't match testnet(dETo)/mainnet(dERo).")
		return false
	}

	// Call NewAddress to confirm address validation from "github.com/deroproject/derosuite/address"
	_, err := address.NewAddress(strings.TrimSpace(addy))
	if err != nil {
		log.Printf("[Util] Address validation failed for '%s': %s", addy, err)
		UtilErrorLogger.Printf("[Util] Address validation failed for '%s': %s", addy, err)
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

func CryptonightHash(shareBuff []byte, diff big.Int) bool {
	var powhash crypto.Hash

	hash := cryptonight.SlowHash(shareBuff)
	copy(powhash[:], hash[:])
	checkPowHashBig := blockchain.CheckPowHashBig(powhash, &diff)

	return checkPowHashBig
}

func AstroBWTHash(shareBuff []byte, diff, setDiff big.Int) (bool, bool) {
	var powhash crypto.Hash
	var data astrobwt.Data

	//hash, success := astrobwt.POW_optimized_v2(shareBuff, max_pow_size, &data)
	hash, _ := astrobwt.POW_optimized_v2(shareBuff, astrobwt.MAX_LENGTH, &data)
	/*
		if !success || hash[len(hash)-1] != 0 {
			return false, false
		}
	*/

	copy(powhash[:], hash[:])

	success := blockchain.CheckPowHashBig(powhash, &setDiff)
	checkPowHashBig := blockchain.CheckPowHashBig(powhash, &diff)

	return checkPowHashBig, success
}

func logFileOutUtil(lType string) *log.Logger {
	var logFileName string
	if lType == "ERROR" {
		logFileName = "logs/utilError.log"
	} else {
		logFileName = "logs/util.log"
	}
	os.Mkdir("logs", 0705)
	f, err := os.OpenFile(logFileName, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0705)
	if err != nil {
		panic(err)
	}

	logType := lType + ": "
	l := log.New(f, logType, log.LstdFlags|log.Lmicroseconds)
	return l
}
