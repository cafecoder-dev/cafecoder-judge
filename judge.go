package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"pack.ag/tftp"
)

var (
	sessionIDChan  chan string
	resultChan     chan bool
	errMessageChan chan string
)

const (
	//BackendHostPort ... appear IP-address and port-number
	BackendHostPort = "localhost:5963"
)

type requestJSON struct {
	SessionID string `json:"sessionID"`
	Command   string `json:"command"`
	Mode      string `json:"mode"` //Mode ... "judge" or "others"
	//Lang      string `json:"lang"` //Lang ... c11,c++17,java8,python3,c#,ruby
}

type cmdResultJSON struct {
	SessionID  string `json:"sessionID"`
	Result     bool   `json:"result"`
	ErrMessage string `json:"errMessage"`
}

type submitT struct {
	sessionID       string //csv[1]
	usercodePath    string
	lang            int
	testcaseDirPath string
	score           int

	execDirPath    string
	execFilePath   string
	testcaseN      int
	testcaseTime   [100]int64
	testcaseResult [100]int

	overallTime   int64
	overallResult int

	containerCli     *client.Client
	containerID      string
	containerInspect types.ContainerJSON

	resultBuffer *bytes.Buffer
	errorBuffer  *bytes.Buffer
}

func checkRegexp(reg, str string) bool {
	return regexp.MustCompile(reg).Match([]byte(str))
}

func fmtWriter(buf *bytes.Buffer, format string, values ...interface{}) {
	arg := fmt.Sprintf(format, values...)
	fmt.Printf(format+"\n", values...)
	(*buf).WriteString(arg + "\n")
}

func passResultTCP(submit submitT, hostAndPort string) {
	conn, err := net.Dial("tcp", hostAndPort)
	if err != nil {
		fmt.Println(err)
		return
	}
	conn.Write([]byte(submit.resultBuffer.String()))
	conn.Write([]byte("error," + submit.sessionID + "," + submit.errorBuffer.String()))
	conn.Close()
}

func containerStopAndRemove(submit submitT) {
	var err error
	//timeout := 5 * time.Second
	err = submit.containerCli.ContainerStop(context.Background(), submit.containerID, nil)
	if err != nil {
		fmtWriter(submit.errorBuffer, "4:%s\n", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, errC := submit.containerCli.ContainerWait(ctx, submit.containerID, "")
	if err := <-errC; err != nil {
		fmt.Println(err)
	}
	err = submit.containerCli.ContainerRemove(context.Background(), submit.containerID, types.ContainerRemoveOptions{RemoveVolumes: true, RemoveLinks: true, Force: true})
	if err != nil {
		fmtWriter(submit.errorBuffer, "5:%s\n", err)
	}

}

func manageCommands() {
	var cmdResult cmdResultJSON
	listen, err := net.Listen("tcp", "0.0.0.0:3344")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
	for {
		cnct, err := listen.Accept()
		if err != nil {
			continue //continue to receive request
		}
		json.NewDecoder(cnct).Decode(&cmdResult)
		cnct.Close()
		println("connection closed")
		sessionIDChan <- cmdResult.SessionID
		resultChan <- cmdResult.Result
		errMessageChan <- cmdResult.ErrMessage
	}
}

func compile(submit *submitT) int {
	var (
		err      error
		requests requestJSON
	)
	containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8888")
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err)
		return -2
	}

	requests.SessionID = submit.sessionID
	submit.execDirPath = "/cafecoderUsers/" + submit.sessionID
	switch submit.lang {
	case 0: //C11
		requests.Command = "gcc" + " /cafecoderUsers/" + submit.sessionID + "/Main.c" + " -lm" + " -std=gnu11" + " -o" + " /cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.out"
	case 1: //C++17
		requests.Command = "g++" + " /cafecoderUsers/" + submit.sessionID + "/Main.cpp" + " -lm" + " -std=gnu++17" + " -o" + " /cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.out"
	case 2: //java8
		requests.Command = "javac" + " /cafecoderUsers/" + submit.sessionID + "/Main.java" + " -d" + " /cafecoderUsers/" + submit.sessionID
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.class"
	case 3: //python3
		requests.Command = "python3" + " -m" + " py_compile" + " /cafecoderUsers/" + submit.sessionID + "/Main.py"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.py"
	case 4: //C#
		requests.Command = "mcs" + " /cafecoderUsers/" + submit.sessionID + "/Main.cs" + " -out:/cafecoderUsers/" + submit.sessionID + "/Main.exe"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.exe"
	case 5: //Ruby
		requests.Command = "ruby" + " -cw" + " /cafecoderUsers/" + submit.sessionID + "/Main.rb"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.rb"
	}

	//I couldn't solve a problem in syntax-chack python3 code.
	//Please teach me how to solve this problem:(
	if submit.lang != 3 && submit.lang != 5 {
		b, _ := json.Marshal(requests)
		containerConn.Write(b)
		if err != nil {
			fmtWriter(submit.errorBuffer, "%s", err)
			return -2
		}
		containerConn.Close()
		for {
			if submit.sessionID == <-sessionIDChan {
				if <-resultChan == false {
					fmtWriter(submit.errorBuffer, "%s\n", <-errMessageChan)
					return -1
				}
				break
			}
		}
	}

	containerConn, err = net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8888")
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err)
		return -2
	}
	requests.Command = "chown rbash_user " + submit.execFilePath + " && " + "chmod 4777 " + submit.execFilePath
	b, _ := json.Marshal(requests)
	containerConn.Write(b)
	containerConn.Close()
	for {
		if submit.sessionID == <-sessionIDChan {
			if <-resultChan == false {
				fmtWriter(submit.errorBuffer, "%s\n", <-errMessageChan)
				return -2
			}
			break
		}
	}

	return 0
}

func tryTestcase(submit *submitT) int {
	var (
		//stderr     bytes.Buffer
		runtimeErr   bool
		parseErr     error
		requests     requestJSON
		testcaseName [256]string
	)
	requests.SessionID = submit.sessionID

	testcaseListFile, err := os.Open(submit.testcaseDirPath + "/testcase_list.txt")
	if err != nil {
		fmtWriter(submit.errorBuffer, "failed to open"+submit.testcaseDirPath+"/testcase_list.txt\n")
		return -1
	}

	scanner := bufio.NewScanner(testcaseListFile)
	for scanner.Scan() {
		testcaseName[submit.testcaseN] = scanner.Text()
		submit.testcaseN++
	}
	testcaseListFile.Close()

	for i := 0; i < submit.testcaseN; i++ {
		testcaseName[i] = strings.TrimSpace(testcaseName[i]) //delete \n\r
		outputTestcase, err := ioutil.ReadFile(submit.testcaseDirPath + "/out/" + testcaseName[i])
		if err != nil {
			fmtWriter(submit.errorBuffer, "%s\n", err)
			return -1
		}
		testcaseFile, _ := os.Open(submit.testcaseDirPath + "/in/" + testcaseName[i])
		submit.containerCli.CopyToContainer(context.Background(), submit.sessionID, "/cafecoderUsers/"+submit.sessionID+"/testcase.txt", bufio.NewReader(testcaseFile), types.CopyToContainerOptions{})
		testcaseFile.Close()

		containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8888")
		if err != nil {
			fmtWriter(submit.errorBuffer, "%s\n", err)
			return -1
		}
		switch submit.lang {
		case 0: //C11
			requests.Command = "sudo -u rbash_user timeout 3 ./cafecoderUsers/" + submit.sessionID + "/Main.out < cafecoderUsers/" + submit.sessionID + "/testcase.txt > cafecoderUsers/" + submit.sessionID + "/userStdout.txt 2> cafecoderUsers/" + submit.sessionID + "/userStderr.txt"
		case 1: //C++17
			requests.Command = "sudo -u rbash_user timeout 3 ./cafecoderUsers/" + submit.sessionID + "/Main.out < cafecoderUsers/" + submit.sessionID + "/testcase.txt > cafecoderUsers/" + submit.sessionID + "/userStdout.txt 2> cafecoderUsers/" + submit.sessionID + "/userStderr.txt"
		case 2: //java8
			requests.Command = "sudo -u rbash_user timeout 3 java -cp /cafecoderUsers/" + submit.sessionID + "/Main < cafecoderUsers/" + submit.sessionID + "/testcase.txt > cafecoderUsers/" + submit.sessionID + "/userStdout.txt 2> cafecoderUsers/" + submit.sessionID + "/userStderr.txt"
		case 3: //python3
			requests.Command = "sudo -u rbash_user timeout 3 python3 /cafecoderUsers/" + submit.sessionID + "/Main.py < cafecoderUsers/" + submit.sessionID + "/testcase.txt > cafecoderUsers/" + submit.sessionID + "/userStdout.txt 2> cafecoderUsers/" + submit.sessionID + "/userStderr.txt"
		case 4: //C#
			requests.Command = "sudo -u rbash_user timeout 3 mono /cafecoderUsers/" + submit.sessionID + "/Main.out < cafecoderUsers/" + submit.sessionID + "/testcase.txt > cafecoderUsers/" + submit.sessionID + "/userStdout.txt 2> cafecoderUsers/" + submit.sessionID + "/userStderr.txt"
		case 5: //Ruby
			requests.Command = "sudo -u rbash_user timeout 3 ./cafecoderUsers/" + submit.sessionID + "/Main.out < cafecoderUsers/" + submit.sessionID + "/testcase.txt > cafecoderUsers/" + submit.sessionID + "/userStdout.txt 2> cafecoderUsers/" + submit.sessionID + "/userStderr.txt"
		}
		requests.Mode = "judge"
		b, _ := json.Marshal(requests)
		containerConn.Write(b)
		containerConn.Close()
		for {
			if submit.sessionID == <-sessionIDChan {
				runtimeErr = <-resultChan
				break
			}
		}

		userStdoutReader, _, err := submit.containerCli.CopyFromContainer(context.TODO(), submit.sessionID, "cafecoderUsers/"+submit.sessionID+"/userStdout.txt")
		if err != nil {
			fmtWriter(submit.errorBuffer, "1:%s\n", err)
			return -1
		}
		userStdout := new(bytes.Buffer)
		userStdout.ReadFrom(userStdoutReader)

		userStderrReader, _, err := submit.containerCli.CopyFromContainer(context.TODO(), submit.sessionID, "cafecoderUsers/"+submit.sessionID+"/userStderr.txt")
		if err != nil {
			fmtWriter(submit.errorBuffer, "2:%s\n", err)
			return -1
		}
		userStderr := new(bytes.Buffer)
		userStdout.ReadFrom(userStderrReader)

		userTimeReader, _, err := submit.containerCli.CopyFromContainer(context.TODO(), submit.sessionID, "cafecoderUsers/"+submit.sessionID+"/userTime.txt")
		if err != nil {
			fmtWriter(submit.errorBuffer, "3:%s\n", err)
			return -1
		}
		userTime := new(bytes.Buffer)
		userTime.ReadFrom(userTimeReader)

		submit.testcaseTime[i], parseErr = strconv.ParseInt(string(userTime.String()), 10, 64)
		if parseErr != nil {
			fmtWriter(submit.errorBuffer, "%s\n", parseErr)
			return -1
		}
		if submit.overallTime < submit.testcaseTime[i] {
			submit.overallTime = submit.testcaseTime[i]
		}

		userStdoutLines := strings.Split(userStdout.String(), "\n")
		userStderrLines := strings.Split(userStderr.String(), "\n")
		outputTestcaseLines := strings.Split(string(outputTestcase), "\n")

		if submit.testcaseTime[i] <= 2000 {
			if runtimeErr == false || userStderr.String() != "" {
				for j := 0; j < len(userStderrLines); j++ {
					fmtWriter(submit.errorBuffer, "%s\n", userStderrLines[j])
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

func executeJudge(csv []string, tftpCli *tftp.Client) {
	var (
		result        = []string{"AC", "WA", "TLE", "RE", "MLE", "CE", "IE"}
		langExtention = [...]string{".c", ".cpp", ".java", ".py", ".cs", ".rb"}
		submit        = submitT{errorBuffer: new(bytes.Buffer), resultBuffer: new(bytes.Buffer)}
		err           error
	)

	/*validation checks*/
	for i := range csv {
		if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, strings.TrimSpace(csv[i])) {
			fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
			fmtWriter(submit.errorBuffer, "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'\n")
			passResultTCP(submit, BackendHostPort)
			return
		}
	}

	if len(csv) > 1 {
		submit.sessionID = csv[1]
	}
	if len(csv) > 6 {
		fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
		fmtWriter(submit.errorBuffer, "too many args\n")
		passResultTCP(submit, BackendHostPort)
		return
	}
	if len(csv) < 6 {
		fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
		fmtWriter(submit.errorBuffer, "too few args\n")
		passResultTCP(submit, BackendHostPort)
		return
	}

	submit.usercodePath = csv[2]
	submit.lang, _ = strconv.Atoi(csv[3])
	submit.testcaseDirPath = csv[4]
	submit.score, _ = strconv.Atoi(csv[5])

	os.Mkdir("cafecoderUsers/"+submit.sessionID, 0777)
	defer os.RemoveAll("cafecoderUsers/" + submit.sessionID)

	//download file
	//submit.code = tftpwrapper.DownloadFromPath(&tftpCli, submit.usercodePath)

	/*--------------------------------about docker--------------------------------*/
	submit.containerCli, err = client.NewClientWithOpts(client.WithVersion("1.35"))
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err)
		passResultTCP(submit, BackendHostPort)
	}
	config := &container.Config{
		Image: "cafecoder",
	}
	resp, err := submit.containerCli.ContainerCreate(context.TODO(), config, nil, nil, strings.TrimSpace(submit.sessionID))
	if err != nil {
		fmtWriter(submit.errorBuffer, "2:%s\n", err)
		passResultTCP(submit, BackendHostPort)
	}
	submit.containerID = resp.ID
	err = submit.containerCli.ContainerStart(context.TODO(), submit.containerID, types.ContainerStartOptions{})
	if err != nil {
		fmtWriter(submit.errorBuffer, "3:%s\n", err)
		passResultTCP(submit, BackendHostPort)
	}

	defer containerStopAndRemove(submit)

	//get container IP address
	submit.containerInspect, _ = submit.containerCli.ContainerInspect(context.TODO(), submit.containerID)
	/*----------------------------------------------------------------------------*/

	containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8888")
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err)
		passResultTCP(submit, BackendHostPort)
		return
	}

	var requests requestJSON
	requests.Command = "mkdir cafecoderUsers/" + submit.sessionID
	requests.SessionID = submit.sessionID
	b, err := json.Marshal(requests)
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err)
		passResultTCP(submit, BackendHostPort)
	}
	containerConn.Write(b)
	containerConn.Close()
	for {
		if submit.sessionID == <-sessionIDChan {
			if <-resultChan == false {
				fmtWriter(submit.errorBuffer, "%s\n", <-errMessageChan)
				passResultTCP(submit, BackendHostPort)
				return
			}
			break
		}
	}

	usercodeFile, _ := os.Open("cafecoderUsers/" + submit.sessionID + "/" + submit.sessionID)
	submit.containerCli.CopyToContainer(
		context.TODO(), submit.containerID,
		"cafecoderUsers/"+submit.sessionID+"/Main"+langExtention[submit.lang],
		usercodeFile, types.CopyToContainerOptions{},
	)
	usercodeFile.Close()

	ret := compile(&submit)
	if ret == -1 {
		fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
		passResultTCP(submit, BackendHostPort)
		return
	} else if ret == -2 {
		fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[5])
		passResultTCP(submit, BackendHostPort)
		return
	}

	ret = tryTestcase(&submit)
	if ret == -1 {
		fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
		passResultTCP(submit, BackendHostPort)
		return
	} else {
		fmtWriter(submit.resultBuffer, "%s,%d,undef,%s,", submit.sessionID, submit.overallTime, result[submit.overallResult])
		if submit.overallResult == 0 {
			fmtWriter(submit.resultBuffer, "%d,", submit.score)
		} else {
			fmtWriter(submit.resultBuffer, "0,")
		}
		for i := 0; i < submit.testcaseN; i++ {
			fmtWriter(submit.resultBuffer, "%s,%d,", result[submit.testcaseResult[i]], submit.testcaseTime[i])
		}
	}
}

func main() {
	go manageCommands()

	listen, err := net.Listen("tcp", "0.0.0.0:8888")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
	tftpCli, err := tftp.NewClient()
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
		go executeJudge(strings.Split(message, ","), tftpCli)
	}
}
