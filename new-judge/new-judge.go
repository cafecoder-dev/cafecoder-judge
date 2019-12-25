package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"./tftpwrapper"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"pack.ag/tftp"
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
	Time       int64  `json:"time"`
	Result     bool   `json:"result"`
	ErrMessage string `json:"errMessage"`
}

type overAllResultJSON struct {
	SessionID     string         `json:"sessionID"`
	OverAllTime   int64          `json:"time"`
	OverAllResult string         `json:"result"`
	OverAllScore  int            `json:"score"`
	ErrMessage    string         `json:"errMessage"`
	Testcases     []testcaseJSON `json:"testcases"`
}

type testcaseJSON struct {
	Name       string `json:"name"`
	Result     string `json:"result"`
	MemoryUsed int64  `json:"memory_used"`
	Time       int64  `json:"time"`
}

type submitT struct {
	sessionID       string //csv[1]
	usercodePath    string
	lang            int
	testcaseDirPath string
	score           int

	compileCmd string
	executeCmd string

	execFilePath       string
	testcaseN          int
	testcaseName       [100]string
	testcaseTime       [100]int64
	testcaseResult     [100]int
	testcaseMemoryUsed [100]int64

	overallTime   int64
	overallResult int

	code             []byte
	containerCli     *client.Client
	containerID      string
	containerInspect types.ContainerJSON

	resultBuffer *bytes.Buffer
	errorBuffer  *bytes.Buffer

	recv cmdResultJSON
}

type commandChicket struct {
	sync.Mutex
	channel map[string]chan cmdResultJSON
}

func checkRegexp(reg, str string) bool {
	return regexp.MustCompile(reg).Match([]byte(str))
}

func fmtWriter(buf *bytes.Buffer, format string, values ...interface{}) {
	arg := fmt.Sprintf(format, values...)
	(*buf).WriteString(arg)
}

func passResultTCP(submit submitT, hostAndPort string) {
	conn, err := net.Dial("tcp", hostAndPort)
	if err != nil {
		fmt.Println(err)
		return
	}
	passStr := strings.Trim(submit.resultBuffer.String(), "\n")
	fmt.Println(passStr)
	conn.Write([]byte(passStr))
	conn.Close()
}

func containerStopAndRemove(submit submitT) {
	var err error
	//timeout := 5 * time.Second
	err = submit.containerCli.ContainerStop(context.Background(), submit.containerID, nil)
	if err != nil {
		fmtWriter(submit.errorBuffer, "4:%s\n", err)
	}
	err = submit.containerCli.ContainerRemove(context.Background(), submit.containerID, types.ContainerRemoveOptions{RemoveVolumes: true, RemoveLinks: true, Force: true})
	if err != nil {
		fmtWriter(submit.errorBuffer, "5:%s\n", err)
	}
	labelFilters := filters.NewArgs()
	submit.containerCli.ContainersPrune(context.Background(), labelFilters)
	fmt.Println("container " + submit.sessionID + " removed")

}

func manageCommands(commandChickets *commandChicket) {
	listen, err := net.Listen("tcp", "0.0.0.0:3344")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
	for {
		cnct, err := listen.Accept()
		if err != nil {
			continue //continue to receive request
		}
		go func() {
			var cmdResult cmdResultJSON
			json.NewDecoder(cnct).Decode(&cmdResult)
			cnct.Close()
			println("connection closed")
			fmt.Println(cmdResult)
			go func() {
				(*commandChickets).channel[cmdResult.SessionID] <- cmdResult
			}()

		}()
	}
}

/*ExecCommandOnContainer ... execute command on container:)*/
func ExecCommandOnContainer(submit *submitT, cmd string, sessionIDChan *chan cmdResultJSON) int {
	var (
		requests requestJSON
		err      error
	)
	containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8887")
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err)
		return 1
	}

	requests.SessionID = submit.sessionID
	requests.Command = cmd

	b, _ := json.Marshal(requests)
	_, err = containerConn.Write(b)
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s", err)
		return 1
	}
	err = containerConn.Close()
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s", err)
		return 1
	}
	fmt.Println("wait for " + cmd + "...")
	for {
		recv := <-*sessionIDChan
		if submit.sessionID == recv.SessionID {
			submit.recv = recv
			break
		}
	}
	return 0
}

func compile(submit *submitT, sessionIDChan *chan cmdResultJSON) int {
	fmt.Println("go compile")
	ret := ExecCommandOnContainer(submit, submit.compileCmd, sessionIDChan)
	if ret != 0 {
		return -1
	}
	if submit.recv.Result == false { //CE
		fmtWriter(submit.errorBuffer, "%s\n", submit.recv.ErrMessage)
		return -2
	}
	fmt.Println("compile done")

	fmt.Println("go chown")
	ret = ExecCommandOnContainer(submit, "chown rbash_user "+submit.execFilePath, sessionIDChan)
	if ret != 0 {
		return -1
	}
	fmtWriter(submit.errorBuffer, "%s\n", submit.recv.ErrMessage)
	fmt.Println("chown done")

	return 0
}

func languageCommand(submit *submitT) {
	switch submit.lang {
	case 0: //C11
		submit.executeCmd = "timeout 3 ./cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.compileCmd = "gcc" + " /cafecoderUsers/" + submit.sessionID + "/Main.c" + " -lm" + " -std=gnu11" + " -o" + " /cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.out"
	case 1: //C++17
		submit.executeCmd = "timeout 3 ./cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.compileCmd = "g++" + " /cafecoderUsers/" + submit.sessionID + "/Main.cpp" + " -lm" + " -std=gnu++17" + " -o" + " /cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.out"
	case 2: //java8
		submit.executeCmd = "timeout 3 java" + " -cp" + " /cafecoderUsers/" + submit.sessionID + " Main"
		submit.compileCmd = "javac" + " /cafecoderUsers/" + submit.sessionID + "/Main.java" + " -d" + " /cafecoderUsers/" + submit.sessionID
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.class"
	case 3: //python3]
		submit.executeCmd = "timeout 3 python3 /cafecoderUsers/" + submit.sessionID + "/Main.py"
		submit.compileCmd = "python3" + " -m" + " py_compile" + " /cafecoderUsers/" + submit.sessionID + "/Main.py"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.py"
	case 4: //C#
		submit.executeCmd = "timeout 3 mono /cafecoderUsers/" + submit.sessionID + "/Main.exe"
		submit.compileCmd = "mcs" + " /cafecoderUsers/" + submit.sessionID + "/Main.cs" + " -out:/cafecoderUsers/" + submit.sessionID + "/Main.exe"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.exe"
		/*
			case 5:
				submit.executeCmd = "timeout 3 ./cafecoderUsers/" + submit.sessionID + "/Main.out"
				submit.compileCmd = "ruby" + " -cw" + " /cafecoderUsers/" + submit.sessionID + "/Main.rb"
				submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.rb"
		*/
	case 6:
		submit.executeCmd = "timeout 3 ./cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.compileCmd = "nim cpp -d:release" + " -o:" + " /cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.out"

	}
}

func tryTestcase(submit *submitT, sessionIDChan *chan cmdResultJSON, overAllResult *overAllResultJSON) int {
	var (
		//stderr     bytes.Buffer
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
		submit.testcaseName[i] = strings.TrimSpace(testcaseName[i])
		outputTestcase, err := ioutil.ReadFile(submit.testcaseDirPath + "/out/" + testcaseName[i])
		if err != nil {
			fmtWriter(submit.errorBuffer, "%s\n", err)
			return -1
		}

		//tar copy
		testcaseFile, _ := os.Open(submit.testcaseDirPath + "/in/" + testcaseName[i])
		content, err := ioutil.ReadAll(testcaseFile)
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		_ = tw.WriteHeader(&tar.Header{
			Name: "/cafecoderUsers/" + submit.sessionID + "/testcase.txt", // filename
			Mode: 0744,                                                    // permissions
			Size: int64(len(content)),                                     // filesize
		})
		tw.Write(content)
		tw.Close()
		submit.containerCli.CopyToContainer(
			context.TODO(),
			submit.containerID,
			"/",
			&buf, types.CopyToContainerOptions{},
		)
		testcaseFile.Close()

		ret := ExecCommandOnContainer(submit, submit.executeCmd, sessionIDChan)
		if ret != 0 {
			fmtWriter(submit.errorBuffer, "%s\n", submit.recv.ErrMessage)
		}
		fmt.Println("time")
		submit.testcaseTime[i] = submit.recv.Time
		fmt.Println(submit.testcaseTime[i])
		if submit.overallTime < submit.testcaseTime[i] {
			submit.overallTime = submit.testcaseTime[i]
		}

		userStdoutReader, _, err := submit.containerCli.CopyFromContainer(context.TODO(), submit.sessionID, "cafecoderUsers/"+submit.sessionID+"/userStdout.txt")
		if err != nil {
			fmtWriter(submit.errorBuffer, "%s\n", err)
			return -1
		}
		tr := tar.NewReader(userStdoutReader)
		tr.Next()
		userStdout := new(bytes.Buffer)
		userStdout.ReadFrom(tr)

		userStderrReader, _, err := submit.containerCli.CopyFromContainer(context.TODO(), submit.sessionID, "cafecoderUsers/"+submit.sessionID+"/userStderr.txt")
		if err != nil {
			fmtWriter(submit.errorBuffer, "%s\n", err)
			return -1
		}
		tr = tar.NewReader(userStderrReader)
		tr.Next()
		userStderr := new(bytes.Buffer)
		userStderr.ReadFrom(tr)

		userStdoutLines := strings.Split(userStdout.String(), "\n")
		userStderrLines := strings.Split(userStderr.String(), "\n")
		outputTestcaseLines := strings.Split(string(outputTestcase), "\n")
		if submit.testcaseTime[i] <= 2000 {
			if userStderr.String() != "" && !submit.recv.Result {
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
	}
	return 0
}

func serveResult(overAllResult *overAllResultJSON, submit submitT, errorMessage string) {
	var result = []string{"AC", "WA", "TLE", "RE", "MLE", "CE", "IE"}
	fmt.Println("submit result")
	overAllResult.SessionID = submit.sessionID
	overAllResult.OverAllResult = result[submit.overallResult]
	overAllResult.OverAllTime = submit.overallTime
	overAllResult.OverAllScore = submit.score
	testcases := make([]testcaseJSON, 0)
	for i := 0; i < submit.testcaseN; i++ {
		var t testcaseJSON
		t.Name = submit.testcaseName[i]
		t.Result = result[submit.testcaseResult[i]]
		t.Time = submit.testcaseTime[i]
		t.MemoryUsed = submit.testcaseMemoryUsed[i]
		testcases = append(testcases, t)
	}
	overAllResult.Testcases = testcases
	//overAllResult.ErrMessage = base64.StdEncoding.EncodeToString([]byte(submit.errorBuffer.String()))
	overAllResult.ErrMessage = submit.errorBuffer.String()
	b, _ := json.Marshal(*overAllResult)
	back := submitT{resultBuffer: new(bytes.Buffer)}
	fmtWriter(back.resultBuffer, "%s", string(b))
	fmt.Println("pass TCP")
	passResultTCP(back, BackendHostPort)
}

func fileCopy(dstName string, srcName string) {
	src, err := os.Open(srcName)
	if err != nil {
		panic(err)
	}
	defer src.Close()

	dst, err := os.Create(dstName)
	if err != nil {
		panic(err)
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	if err != nil {
		panic(err)
	}
}

func initSubmit(submit *submitT) {
	submit.overallTime = -1
}

func createContainer(submit *submitT, overAllResult overAllResultJSON) error {
	var err error
	err = nil
	/*--------------------------------about docker--------------------------------*/
	submit.containerCli, err = client.NewClientWithOpts(client.WithVersion("1.35"))
	if err != nil {
		return err
	}
	config := &container.Config{
		Image: "cafecoder",
	}
	resp, err := submit.containerCli.ContainerCreate(context.TODO(), config, nil, nil, strings.TrimSpace(submit.sessionID))
	if err != nil {
		return err
	}
	submit.containerID = resp.ID
	err = submit.containerCli.ContainerStart(context.TODO(), submit.containerID, types.ContainerStartOptions{})
	if err != nil {
		return err
	}

	//get container IP address
	submit.containerInspect, _ = submit.containerCli.ContainerInspect(context.TODO(), submit.containerID)
	/*----------------------------------------------------------------------------*/

	return nil
}

func executeJudge(csv []string, tftpCli **tftp.Client, commandChickets *map[string]chan cmdResultJSON) {
	var (
		//result        = []string{"AC", "WA", "TLE", "RE", "MLE", "CE", "IE"}
		langExtention = [...]string{".c", ".cpp", ".java", ".py", ".cs", ".rb", ".nim"}
		submit        = submitT{errorBuffer: new(bytes.Buffer), resultBuffer: new(bytes.Buffer)}
		err           error
		overAllResult overAllResultJSON
	)
	initSubmit(&submit)

	submit.overallResult = 0
	if len(csv) > 1 {
		submit.sessionID = csv[1]
	}
	if len(csv) > 6 {
		submit.overallResult = 6
		serveResult(&overAllResult, submit, "too many args")
		return
	}
	if len(csv) < 6 {
		submit.overallResult = 6
		serveResult(&overAllResult, submit, "too few args")
		return
	}

	/*validation checks*/
	for i := range csv {
		if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, strings.TrimSpace(csv[i])) {
			submit.overallResult = 6
			serveResult(&overAllResult, submit, "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'")
			return
		}
	}

	submit.usercodePath = csv[2]
	submit.lang, _ = strconv.Atoi(csv[3])
	submit.testcaseDirPath = csv[4]
	submit.score, _ = strconv.Atoi(csv[5])
	languageCommand(&submit)
	sessionIDChan := (*commandChickets)[submit.sessionID]
	defer func() { delete((*commandChickets), submit.sessionID) }()

	//download file
	submit.code = tftpwrapper.DownloadFromPath(tftpCli, submit.usercodePath)

	os.Mkdir("cafecoderUsers/"+submit.sessionID, 0777)
	file, err := os.Create("cafecoderUsers/" + submit.sessionID + "/" + submit.sessionID)
	file.Write(submit.code)
	file.Close()
	//fileCopy("cafecoderUsers/"+submit.sessionID+"/"+submit.sessionID, submit.usercodePath)
	defer os.Remove("cafecoderUsers/" + submit.sessionID)

	err = createContainer(&submit, overAllResult)
	if err != nil {
		pass
	}
	defer containerStopAndRemove(submit)

	println("check")
	containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8887")
	if err != nil {
		//fmtWriter(submit.errorBuffer, "%s\n", err)
		//passResultTCP(submit, BackendHostPort)
		submit.overallResult = 6
		serveResult(&overAllResult, submit, err.Error())
		return
	}

	var requests requestJSON
	requests.Command = "mkdir -p cafecoderUsers/" + submit.sessionID
	requests.SessionID = submit.sessionID
	b, err := json.Marshal(requests)
	if err != nil {
		//fmtWriter(submit.errorBuffer, "%s\n", err)
		//passResultTCP(submit, BackendHostPort)
		submit.overallResult = 6
		serveResult(&overAllResult, submit, err.Error())
		return
	}
	println(string(b))
	containerConn.Write(b)
	containerConn.Close()
	for {
		recv := <-sessionIDChan
		if submit.sessionID == recv.SessionID {
			fmtWriter(submit.errorBuffer, "%s\n", recv.ErrMessage)
			break
		}
	}
	println("check")
	//tar copy
	usercodeFile, _ := os.Open("cafecoderUsers/" + submit.sessionID + "/" + submit.sessionID)
	content, err := ioutil.ReadAll(usercodeFile)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{
		Name: "cafecoderUsers/" + submit.sessionID + "/Main" + langExtention[submit.lang], // filename
		Mode: 0777,                                                                        // permissions
		Size: int64(len(content)),                                                         // filesize
	})
	tw.Write(content)
	tw.Close()
	submit.containerCli.CopyToContainer(
		context.TODO(), submit.containerID,
		"/",
		&buf, types.CopyToContainerOptions{},
	)
	usercodeFile.Close()
	//
	ret := compile(&submit, &sessionIDChan)
	if ret == -1 {
		//fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
		//passResultTCP(submit, BackendHostPort)
		submit.overallResult = 6
		serveResult(&overAllResult, submit, "")
		return
	} else if ret == -2 {
		//fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[5])
		//passResultTCP(submit, BackendHostPort)
		submit.overallResult = 5
		serveResult(&overAllResult, submit, "")
		return
	}

	ret = tryTestcase(&submit, &sessionIDChan, &overAllResult)
	fmt.Println("test done")
	if ret == -1 {
		//fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
		//passResultTCP(submit, BackendHostPort)
		submit.overallResult = 6
		serveResult(&overAllResult, submit, "")
		return
	}
	//fmtWriter(submit.resultBuffer, "%s,%d,undef,%s,", submit.sessionID, submit.overallTime, result[submit.overallResult])
	/*
		if submit.overallResult == 0 {
			fmtWriter(submit.resultBuffer, "%d,", submit.score)
		} else {
			fmtWriter(submit.resultBuffer, "0,")
		}
		for i := 0; i < submit.testcaseN; i++ {
			fmtWriter(submit.resultBuffer, "%s,%d,", result[submit.testcaseResult[i]], submit.testcaseTime[i])
		}
		passResultTCP(submit, BackendHostPort)
	*/
	serveResult(&overAllResult, submit, "")
	return
}

func main() {
	commandChickets := commandChicket{channel: make(map[string]chan cmdResultJSON)}
	go manageCommands(&commandChickets)
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
		session := strings.Split(message, ",")
		if len(session) <= 2 {
			println(session)
			continue
		}
		commandChickets.channel[session[1]] = make(chan cmdResultJSON)
		go executeJudge(session, &tftpCli, &commandChickets.channel)
	}
}
