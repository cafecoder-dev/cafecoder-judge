package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"regexp"
	"os/exec"
	"bytes"
	"strings"
	"io/ioutil"
	"strconv"
)

type submitT struct {
	sessionID       string
	usercodePath    string
	testcaseDirPath string
	execDirPath     string
	execFilePath    string
	score           int

	//0:C 1:C++ 2:Java8 3:Python3 4:C#
	lang          int
	langExtention string

	//0:AC 1:WA 2:TLE 3:RE 4:MLE 5:CE 6:IE *Please reference atcoder.
	testcaseResult [100]int
	overallResult  int

	testcaseTime [100]int64
	overallTime  int64
	testcaseCnt  int
	memoryUsed   int
	errBuffer    *bytes.Buffer
	resultBuffer    *bytes.Buffer
}
const (
	BACKEND_HOST_PORT = "localhost:5963"
)
func checkRegexp(reg, str string) bool {
	return regexp.MustCompile(reg).Match([]byte(str))
}

func fmtWriter(buf *bytes.Buffer, format string, values ... interface{}) {
	arg := fmt.Sprintf(format, values...)
	fmt.Printf(format + "\n",values...)
	(*buf).WriteString(arg + "\n")
}

func passResultTCP(submit submitT, hostAndPort string){
	conn , err := net.Dial("tcp", hostAndPort)
	if err != nil {
		fmt.Println(err)
		return
	}
	conn.Write([]byte(submit.resultBuffer.String()))
	conn.Close()
	conn , err = net.Dial("tcp", hostAndPort)
	if err != nil {
		fmt.Println(err)
		return
	}
	conn.Write([]byte("error," + submit.sessionID +"," + submit.errBuffer.String()))
	conn.Close()
}

func compile(submit *submitT) int {
	var (
		compileCmd *exec.Cmd
		stderr     bytes.Buffer
	)

	mkdirCmd := exec.Command("docker", "exec", "-i", "ubuntuForJudge", "/bin/bash", "-c", "mkdir cafecoderUsers/"+submit.sessionID)
	mkdirCmd.Stderr = &stderr
	err := mkdirCmd.Run()
	if err != nil {
		fmtWriter(submit.errBuffer, "couldn't execute next command \"mkdir cafecoderUsers/****\"\n")
		fmtWriter(submit.errBuffer, "%s\n", stderr.String())
		return -2
	}
	os.Mkdir("../judge_server/tmp/"+submit.sessionID, 0777)

	cpCmd := exec.Command("docker", "cp", submit.usercodePath, "ubuntuForJudge:/cafecoderUsers/"+submit.sessionID+"/Main"+submit.langExtention)
	cpCmd.Stderr = &stderr
	err = cpCmd.Run()
	if err != nil {
		fmtWriter(submit.errBuffer, "%s", stderr.String())
	}
	switch submit.lang {
	case 0: //C11
		compileCmd = exec.Command("docker", "exec", "-i", "ubuntuForJudge", "gcc", "/cafecoderUsers/"+submit.sessionID+"/Main.c", "-lm", "-std=gnu11", "-o", "/cafecoderUsers/"+submit.sessionID+"/Main.out")
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.execDirPath = "/cafecoderUsers/" + submit.sessionID
	case 1: //C++17
		compileCmd = exec.Command("docker", "exec", "-i", "ubuntuForJudge", "g++", "/cafecoderUsers/"+submit.sessionID+"/Main.cpp", "-lm", "-std=gnu++17", "-o", "/cafecoderUsers/"+submit.sessionID+"/Main.out")
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.execDirPath = "/cafecoderUsers/" + submit.sessionID
	case 2: //java8
		compileCmd = exec.Command("docker", "exec", "-i", "ubuntuForJudge", "javac", "/cafecoderUsers/"+submit.sessionID+"/Main.java", "-d", "/cafecoderUsers/"+submit.sessionID)
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.class"
		submit.execDirPath = "/cafecoderUsers/" + submit.sessionID
	case 3: //python3
		compileCmd = exec.Command("docker", "exec", "-i", "ubuntuForJudge", "python3", "-m", "py_compile", "/cafecoderUsers/"+submit.sessionID+"/Main.py")
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.py"
		submit.execDirPath = "/cafecoderUsers/" + submit.sessionID
	case 4: //C#
		compileCmd = exec.Command("docker", "exec", "-i", "ubuntuForJudge", "mcs", "/cafecoderUsers/"+submit.sessionID+"/Main.cs", "-out:/cafecoderUsers/"+submit.sessionID+"/Main.exe")
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.exe"
		submit.execDirPath = "/cafecoderUsers/" + submit.sessionID
	case 5: //Ruby
		compileCmd = exec.Command("docker", "exec", "-i", "ubuntuForJudge", "ruby", "-cw", "/cafecoderUsers/"+submit.sessionID+"/Main.rb")
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.rb"
		submit.execDirPath = "/cafecoderUsers/" + submit.sessionID
	}

	compileCmd.Stderr = &stderr
	if submit.lang != 5 {
		err = compileCmd.Run()
		if err != nil {
			fmtWriter(submit.errBuffer, "%s\n", stderr.String())
			return -1
		}
	}

	chownErr := exec.Command("docker", "exec", "-i", "ubuntuForJudge", "chown", "rbash_user", submit.execFilePath).Run()
	chmodErr := exec.Command("docker", "exec", "-i", "ubuntuForJudge", "chmod", "4777", submit.execFilePath).Run()
	if chownErr != nil || chmodErr != nil {
		fmtWriter(submit.errBuffer, "failed to give permission\n")
		return -2
	}

	return 0
}

func tryTestcase(submit *submitT) int {
	var (
		stderr     bytes.Buffer
		runtimeErr error
	)

	testcaseListFp, err := os.Open(submit.testcaseDirPath + "/testcase_list.txt")
	if err != nil {
		fmtWriter(submit.errBuffer, "failed to open"+submit.testcaseDirPath+"/testcase_list.txt\n")
		return -1
	}

	defer testcaseListFp.Close()

	var testcaseName [256]string
	scanner := bufio.NewScanner(testcaseListFp)
	testcaseN := 0
	for scanner.Scan() {
		testcaseName[testcaseN] = scanner.Text()
		testcaseN++
	}
	submit.testcaseCnt = testcaseN

	submit.overallTime = 0
	submit.overallResult = 0

	if err != nil {
		fmtWriter(submit.errBuffer, "%s\n", err)
		return -1
	}

	for i := 0; i < testcaseN; i++ {
		testcaseName[i] = strings.TrimSpace(testcaseName[i]) //delete \n\r
		outputTestcase, err := ioutil.ReadFile(submit.testcaseDirPath + "/out/" + testcaseName[i])
		if err != nil {
			fmtWriter(submit.errBuffer, "%s\n", err)
			return -1
		}

		testcaseCpCmd := exec.Command("docker", "cp", submit.testcaseDirPath+"/in/"+testcaseName[i], "ubuntuForJudge:/cafecoderUsers/"+submit.sessionID+"/testcase.txt")
		err = testcaseCpCmd.Run()
		if err != nil {
			fmtWriter(submit.errBuffer, "%s\n", err)
			return -1
		}

		executeUsercodeCmd := exec.Command("docker", "exec", "-i", "ubuntuForJudge", "./executeUsercode.sh", strconv.Itoa(submit.lang), submit.sessionID)
		runtimeErr = executeUsercodeCmd.Run()

		exec.Command("docker", "cp", "ubuntuForJudge:/cafecoderUsers/"+submit.sessionID+"/.", "../judge_server/tmp/"+submit.sessionID).Run()
		userStdout, err := exec.Command("cat", "../judge_server/tmp/"+submit.sessionID+"/userStdout.txt").Output()
		if err != nil {
			fmtWriter(submit.errBuffer, "1:%s\n", stderr.String())
			return -1
		}
		userStderr, err := exec.Command("cat", "../judge_server/tmp/"+submit.sessionID+"/userStderr.txt").Output()
		if err != nil {
			fmtWriter(submit.errBuffer, "2:%s\n", err)
			return -1
		}
		userTime, err := exec.Command("cat", "../judge_server/tmp/"+submit.sessionID+"/userTime.txt").Output()
		if err != nil {
			fmtWriter(submit.errBuffer, "3:%s\n", err)
			return -1
		}

		var tmpInt64 int64
		tmpInt64, parseerr := strconv.ParseInt(string(userTime), 10, 64)
		submit.testcaseTime[i] = tmpInt64
		if parseerr != nil {
			fmtWriter(submit.errBuffer, "%s\n", string(userTime))
			fmtWriter(submit.errBuffer, "%s\n", parseerr)
			return -1
		}
		if submit.overallTime < submit.testcaseTime[i] {
			submit.overallTime = submit.testcaseTime[i]
		}

		userStdoutLines := strings.Split(string(userStdout), "\n")
		userStderrLines := strings.Split(string(userStderr), "\n")
		outputTestcaseLines := strings.Split(string(outputTestcase), "\n")

		if submit.testcaseTime[i] <= 2000 {
			if runtimeErr != nil || string(userStderr) != "" {
				for j := 0; j < len(userStderrLines); j++ {
					fmtWriter(submit.errBuffer, "%s\n", userStderrLines[j])
				}
				submit.testcaseResult[i] = 3 //RE
			} else {
				submit.testcaseResult[i] = 1 //WA
				for j := 0; j < len(userStdoutLines) && j < len(outputTestcaseLines); j++ {
					submit.testcaseResult[i] = 0 //AC
					if strings.TrimSpace(string(userStdoutLines[j])) != strings.TrimSpace(string(outputTestcaseLines[j])) {
						submit.testcaseResult[i] = 1 //WA
						break
					}
					/*
						if string(userStdoutLines[j]) != string(outputTestcaseLines[j]) {
							submit.testcaseResult[i] = 1 //WA
							break
						}
					*/
				}
			}
		} else {
			submit.testcaseResult[i] = 2 //TLE
		}
		if submit.testcaseResult[i] > submit.overallResult {
			submit.overallResult = submit.testcaseResult[i]
		}
	}
	return 0
}

func deleteUserDir(submit submitT) {
	exec.Command("docker", "exec", "-i", "ubuntuForJudge", "rm", "-r", "cafecoderUsers/"+submit.sessionID).Run()
	exec.Command("rm", "-r", "../judge_server/tmp/"+submit.sessionID).Run()
}

func deleteUserCode(submit submitT) {
	exec.Command("docker", "exec", "-i", "ubuntuForJudge", "rm", "cafecoderUsers/"+submit.sessionID+"/Main"+submit.langExtention).Run()
}

func executeJudge(csv []string) {
	var (
		result = []string{"AC", "WA", "TLE", "RE", "MLE", "CE", "IE"}
		lang   = [...]string{".c", ".cpp", ".java", ".py", ".cs", ".rb"}
		submit = submitT{errBuffer: new(bytes.Buffer), resultBuffer: new(bytes.Buffer)}
		args   = csv 
	)

	if len(args) > 6 {
		fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
		fmtWriter(submit.errBuffer, "too many args\n")
		passResultTCP(submit, BACKEND_HOST_PORT)
		return
	} else if len(args) < 6 {
		fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
		fmtWriter(submit.errBuffer, "too few args\n")
		passResultTCP(submit, BACKEND_HOST_PORT)
		return
	}

	/*validation_chack*/
	submit.sessionID = args[1]
	for i := 2; i <= 5; i++ {
		if checkRegexp("[^(A-Z|a-z|0-9|_|/|.)]", args[i]) == true {
			fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
			fmtWriter(submit.errBuffer, "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'\n")
			passResultTCP(submit, BACKEND_HOST_PORT)
			return
		}
	}

	submit.usercodePath = args[2]
	submit.lang, _ = strconv.Atoi(args[3])
	submit.testcaseDirPath = args[4]
	submit.score, _ = strconv.Atoi(args[5])
	submit.langExtention = lang[submit.lang]

	defer deleteUserDir(submit)

	ret := compile(&submit)
	if ret == -2 {
		fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
		passResultTCP(submit, BACKEND_HOST_PORT)
		return
	} else if ret == -1 {
		fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[5])
		passResultTCP(submit, BACKEND_HOST_PORT)
		return
	}
	ret = tryTestcase(&submit)
	if ret == -1 {
		fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
		passResultTCP(submit, BACKEND_HOST_PORT)
		return
	} else {
		fmtWriter(submit.resultBuffer, "%s,%d,undef,%s,", submit.sessionID, submit.overallTime, result[submit.overallResult])
		if submit.overallResult == 0 {
			fmtWriter(submit.resultBuffer, "%d,", submit.score)
		} else {
			fmtWriter(submit.resultBuffer, "0,")
		}
		for i := 0; i < submit.testcaseCnt; i++ {
			fmtWriter(submit.resultBuffer, "%s,%d,", result[submit.testcaseResult[i]], submit.testcaseTime[i])
		}
	}
}

func main() {
	listen, err := net.Listen("tcp", "0.0.0.0:8888") //from backend server
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
	for {
		cnct, err := listen.Accept()
		if err != nil {
			continue //continue to receive request
		}
		message, err := bufio.NewReader(cnct).ReadString('\n')
		println(string(message))
		//reader := csv.NewReader(messageLen)
		cnct.Close()
		println("connection closed")
		go executeJudge(strings.Split("dummy,"+message, ","))
	}
}
