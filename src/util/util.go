package util

import (
	"fmt"
	"time"
	"regexp"
	"crypto/sha256"
	"encoding/hex"

	"github.com/cafecoder-dev/cafecoder-judge/src/types"
)

func ValidationCheck(args types.SubmitsGORM) bool {
	if !CheckRegexp(`[(A-Za-z0-9\./_\/)]*`, args.Lang) || !CheckRegexp(`[(A-Za-z0-9\./_\/)]*`, args.Path) {
		return false
	}
	return true
	//"Inputs are included another characters[0-9], [a-z], [A-Z], '.', '/', '_'"
}

func CheckRegexp(reg, str string) bool {
	compiled, err := regexp.Compile(reg)
	if err != nil {
		fmt.Println(err)
		return false
	}

	return compiled.Match([]byte(str))
}

func TimeToString(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func MakeStringHash(str string) string {
	hash := sha256.Sum256([]byte(str))
	return hex.EncodeToString(hash[:])
}