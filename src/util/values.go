package util

// JudgeNumberLimit ... limits the number of judges
//
// see: https://mattn.kaoriya.net/software/lang/go/20171221111857.htm
var JudgeNumberLimit = make(chan struct{}, MaxJudge)

// MaxJudge ... 同時にジャッジできる最大の数
const MaxJudge = 5