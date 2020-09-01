package checklib

import (
	"strings"
	"fmt"
)

// https://qiita.com/spiegel-im-spiegel/items/f1cc014ecb233afaa8af
// "\r\n", "\r", "\n" を afterStr で置き換える
func convNewline(str, afterStr string) string {
    return strings.NewReplacer(
        "\r\n", afterStr,
        "\r", afterStr,
        "\n", afterStr,
    ).Replace(str)
}

// 一致判定のみ行う。戻り値は bool 型
func Normal(userOutput string, testOutput string) bool {
	userOutput = convNewline(userOutput, " ")
	testOutput = convNewline(testOutput, " ")

	userOutputLines := strings.Split(userOutput, " ")
	testOutputLines := strings.Split(testOutput, " ")

	var i, j int
	fmt.Printf("%d %d\n",len(userOutputLines) , len(testOutputLines) )
	for  {
		var str1, str2 string
		for str1 == "" {
			if i >= len(userOutputLines) { 
				str1 = " "
			} else {
				str1 = userOutputLines[i]
				i++
			}
		}
		for str2 == "" {
			if j >= len(testOutputLines) { 
				str2 = " "
			} else {
				str2 = testOutputLines[j]
				j++
			}
		}

		if str1 == " " && str2 == " " {break}

		fmt.Printf("%s|%s|\n", str1, str2)

		if str1 != str2 {
			return false
		}
	}

	return true
}