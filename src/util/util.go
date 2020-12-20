package util

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"

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

func MakeRandomString(digit int) (string, error) {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	// 乱数を生成
	b := make([]byte, digit)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	// letters からランダムに取り出して文字列を生成
	var result string
	for _, v := range b {
		// index が letters の長さに収まるように調整
		result += string(letters[int(v)%len(letters)])
	}
	return result, nil
}
