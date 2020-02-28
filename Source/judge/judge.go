package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
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
	BackendHostPort = "133.130.101.250:5963"
)

type requestJSON struct {
	SessionID     string `json:"sessionID"`
	Command       string `json:"command"`
	Mode          string `json:"mode"` //Mode ... "judge" or "others"
	DirectoryName string `json:"directoryName"`
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

	compileCmd   string
	executeCmd   string
	execDirPath  string
	execFilePath string

	directoryName string

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
			data, _ := base64.StdEncoding.DecodeString(cmdResult.ErrMessage)
			cmdResult.ErrMessage = string(data)
			go func() {
				(*commandChickets).channel[cmdResult.SessionID] <- cmdResult
			}()

		}()
	}
}

func langConfig(submit *submitT) {
	submit.execDirPath = "/cafecoderUsers/" + submit.directoryName
	switch submit.lang {
	case 0: //C11
		submit.compileCmd = "gcc" + " /cafecoderUsers/" + submit.directoryName + "/Main.c" + " -O2 " + " -lm" + " -std=gnu11" + " -o" + " /cafecoderUsers/" + submit.directoryName + "/Main.out"
		submit.execFilePath = "/cafecoderUsers/" + submit.directoryName + "/Main.out"
		submit.executeCmd = "./cafecoderUsers/" + submit.directoryName + "/Main.out"
	case 1: //C++17
		submit.compileCmd = "g++" + " /cafecoderUsers/" + submit.directoryName + "/Main.cpp" + " -O2 " + " -lm" + " -std=gnu++17" + " -o" + " /cafecoderUsers/" + submit.directoryName + "/Main.out"
		submit.execFilePath = "/cafecoderUsers/" + submit.directoryName + "/Main.out"
		submit.executeCmd = "./cafecoderUsers/" + submit.directoryName + "/Main.out"
	case 2: //java8
		submit.compileCmd = "javac" + " /cafecoderUsers/" + submit.directoryName + "/Main.java" + " -d" + " /cafecoderUsers/" + submit.directoryName
		submit.execFilePath = "/cafecoderUsers/" + submit.directoryName + "/Main.class"
		submit.executeCmd = "java" + " -cp" + " /cafecoderUsers/" + submit.directoryName + " Main"
	case 3: //python3
		submit.compileCmd = "python3" + " -m" + " py_compile" + " /cafecoderUsers/" + submit.directoryName + "/Main.py"
		submit.execFilePath = "/cafecoderUsers/" + submit.directoryName + "/Main.py"
		submit.executeCmd = "python3 /cafecoderUsers/" + submit.directoryName + "/Main.py"
	case 4: //C#
		submit.compileCmd = "mcs" + " /cafecoderUsers/" + submit.directoryName + "/Main.cs" + " -out:/cafecoderUsers/" + submit.directoryName + "/Main.exe"
		submit.execFilePath = "/cafecoderUsers/" + submit.directoryName + "/Main.exe"
		submit.executeCmd = "mono /cafecoderUsers/" + submit.directoryName + "/Main.exe"
	case 5: //golang
		submit.compileCmd = "go build " + submit.directoryName + " /Main.go -o " + "/cafecoderUsers/" + submit.directoryName + "/Main.out"
		submit.execFilePath = "/cafecoderUsers/" + submit.directoryName + "/Main.out"
		submit.executeCmd = "./cafecoderUsers/" + submit.directoryName + "/Main.out"

	}

}

func compile(submit *submitT, sessionIDChan *chan cmdResultJSON) int {
	var (
		err      error
		requests requestJSON
	)
	containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8887")
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err)
		return -2
	}

	requests.SessionID = submit.sessionID

	if submit.lang != 5 {
		fmt.Println("go compile")
		b, _ := json.Marshal(requests)
		containerConn.Write(b)
		if err != nil {
			fmtWriter(submit.errorBuffer, "%s", err)
			return -2
		}
		containerConn.Close()
		fmt.Println("wait for compile...")
		for {
			recv := <-*sessionIDChan
			if submit.sessionID == recv.SessionID {
				fmtWriter(submit.errorBuffer, "%s\n", recv.ErrMessage)
				if !recv.Result {
					//CE
					return -2
				}
				break
			}
		}
		fmt.Println("compile done")
	}

	containerConn, err = net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8887")
	defer containerConn.Close()
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err)
		return -2
	}
	requests.Command = "chown rbash_user " + submit.execFilePath
	b, _ := json.Marshal(requests)
	containerConn.Write(b)
	containerConn.Close()
	for {
		fmt.Println("wating for chwon")
		recv := <-*sessionIDChan
		if submit.sessionID == recv.SessionID {
			fmtWriter(submit.errorBuffer, "%s\n", recv.ErrMessage)
			break
		}
	}

	return 0
}

func tryTestcase(submit *submitT, sessionIDChan *chan cmdResultJSON, overAllResult *overAllResultJSON) int {
	var (
		//stderr     bytes.Buffer
		requests     requestJSON
		testcaseName [256]string
		TLEcase      bool
	)
	TLEcase = false
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
	fmt.Printf("N = %d", submit.testcaseN)

	for i := 0; i < submit.testcaseN; i++ {
		testcaseName[i] = strings.TrimSpace(testcaseName[i]) //delete \n\r
		submit.testcaseName[i] = strings.TrimSpace(testcaseName[i])
		if TLEcase{
			submit.testcaseResult[i]=7;//-
			submit.testcaseTime[i]=0;
			continue;
		}
		outputTestcase, err := ioutil.ReadFile(submit.testcaseDirPath + "/out/" + testcaseName[i])
		if err != nil {
			fmt.Printf("272.readfile error : %s\n", err)
			fmtWriter(submit.errorBuffer, "272.readfile error : %s\n", err)
			return -1
		}

		//tar copy
		testcaseFile, _ := os.Open(submit.testcaseDirPath + "/in/" + testcaseName[i])
		content, err := ioutil.ReadAll(testcaseFile)
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		_ = tw.WriteHeader(&tar.Header{
			Name: "/cafecoderUsers/" + submit.directoryName + "/testcase.txt", // filename
			Mode: 0744,                                                        // permissions
			Size: int64(len(content)),                                         // filesize
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

		containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8887")
		if err != nil {
			fmt.Printf("298.readfile error : %s\n", err)
			fmtWriter(submit.errorBuffer, "298.readfile error : %s\n", err)
			return -1
		}

		requests.Mode = "judge"
		requests.DirectoryName = submit.directoryName
		b, _ := json.Marshal(requests)
		containerConn.Write(b)
		containerConn.Close()
		fmt.Println("wait for testcase...")
		var recv cmdResultJSON
		for {
			recv = <-*sessionIDChan
			if submit.sessionID == recv.SessionID {
				break
			}
		}
		fmt.Println(recv)

		userStdoutReader, _, err := submit.containerCli.CopyFromContainer(context.TODO(), submit.sessionID, "cafecoderUsers/"+submit.directoryName+"/userStdout.txt")
		if err != nil {
			fmt.Printf("330.cp error :%s\n", err)
			fmtWriter(submit.errorBuffer, "330.cp error :%s\n", err)
			return -1
		}
		tr := tar.NewReader(userStdoutReader)
		tr.Next()
		userStdout := new(bytes.Buffer)
		userStdout.ReadFrom(tr)

		userStderrReader, _, err := submit.containerCli.CopyFromContainer(context.TODO(), submit.sessionID, "cafecoderUsers/"+submit.directoryName+"/userStderr.txt")
		if err != nil {
			fmt.Printf("340.cp error :%s\n", err)
			fmtWriter(submit.errorBuffer, "340.cp error :%s\n", err)
			return -1
		}
		tr = tar.NewReader(userStderrReader)
		tr.Next()
		userStderr := new(bytes.Buffer)
		userStderr.ReadFrom(tr)
		fmt.Println("time")
		submit.testcaseTime[i] = recv.Time
		fmt.Println(submit.testcaseTime[i])
		if submit.overallTime < submit.testcaseTime[i] {
			submit.overallTime = submit.testcaseTime[i]
		}

		userStdoutLines := strings.Split(userStdout.String(), "\n")
		userStderrLines := strings.Split(userStderr.String(), "\n")
		outputTestcaseLines := strings.Split(string(outputTestcase), "\n")

		if submit.testcaseTime[i] <= 2000 {
			if userStderr.String() != "" && !recv.Result {
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
			TLEcase = true
		}
		if submit.testcaseResult[i] > submit.overallResult {
			submit.overallResult = submit.testcaseResult[i]
		}

		userStderrReader.Close()
		userStdoutReader.Close()
	}

	return 0
}

func serveResult(overAllResult *overAllResultJSON, submit submitT, errorMessage string) {
	var result = []string{"AC", "WA", "TLE", "RE", "MLE", "CE", "IE", "-"}
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
	if submit.overallResult == 5 { //output errMessage if result is CE
		overAllResult.ErrMessage = base64.StdEncoding.EncodeToString([]byte(submit.errorBuffer.String()))
	} else {
		overAllResult.ErrMessage = ""
	}
	//overAllResult.ErrMessage = submit.errorBuffer.String()
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

func executeJudge(csv []string, tftpCli **tftp.Client, commandChickets *map[string]chan cmdResultJSON) {
	var (
		//result        = []string{"AC", "WA", "TLE", "RE", "MLE", "CE", "IE"}
		langExtention = [...]string{".c", ".cpp", ".java", ".py", ".cs", ".rb"}
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
		/*
			fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
			fmtWriter(submit.errorBuffer, "too many args\n")
			passResultTCP(submit, BackendHostPort)
		*/
		println("too many args")
		submit.overallResult = 6
		serveResult(&overAllResult, submit, "too many args")
		return
	}
	if len(csv) < 6 {
		/*
				fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
				fmtWriter(submit.errorBuffer, "too few args\n")
			passResultTCP(submit, BackendHostPort)
		*/
		submit.overallResult = 6
		println("too few args")
		serveResult(&overAllResult, submit, "too few args")
		return
	}

	/*validation checks*/
	for i := range csv {
		if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, strings.TrimSpace(csv[i])) {
			//fmtWriter(submit.resultBuffer, "%s,-1,undef,%s,0,", submit.sessionID, result[6])
			//fmtWriter(submit.errorBuffer, "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'\n")
			//passResultTCP(submit, BackendHostPort)
			submit.overallResult = 6
			println("Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'")
			serveResult(&overAllResult, submit, "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'")
			return
		}
	}

	submit.usercodePath = csv[2]
	submit.lang, _ = strconv.Atoi(csv[3])
	submit.testcaseDirPath = csv[4]
	submit.score, _ = strconv.Atoi(csv[5])
	sessionIDChan := (*commandChickets)[submit.sessionID]
	defer func() { delete((*commandChickets), submit.sessionID) }()
	hash := sha256.Sum256([]byte(submit.sessionID))
	submit.directoryName = hex.EncodeToString(hash[:])

	langConfig(&submit)

	//println(submit.usercodePath)

	//download file
	submit.code = tftpwrapper.DownloadFromPath(tftpCli, submit.usercodePath)

	os.Mkdir("cafecoderUsers/"+submit.directoryName, 0777)
	file, err := os.Create("cafecoderUsers/" + submit.directoryName + "/" + submit.directoryName)
	file.Write(submit.code)
	file.Close()
	//fileCopy("cafecoderUsers/"+submit.sessionID+"/"+submit.sessionID, submit.usercodePath)
	defer exec.Command("rm", "-r", "cafecoderUsers/*").Run()

	/*--------------------------------about docker--------------------------------*/
	submit.containerCli, err = client.NewClientWithOpts(client.WithVersion("1.35"))
	defer submit.containerCli.Close()
	if err != nil {
		//fmtWriter(submit.errorBuffer, "%s\n", err)
		//passResultTCP(submit, BackendHostPort)
		submit.overallResult = 6
		println("container error")
		serveResult(&overAllResult, submit, err.Error())
		return
	}

	config := &container.Config{

		Image: "cafecoder",
	}
	resp, err := submit.containerCli.ContainerCreate(context.TODO(), config, nil, nil, strings.TrimSpace(submit.sessionID))
	if err != nil {
		//fmtWriter(submit.errorBuffer, "2:%s\n", err)
		//passResultTCP(submit, BackendHostPort)
		submit.overallResult = 6
		println(err.Error())
		serveResult(&overAllResult, submit, err.Error())
		return
	}

	submit.containerID = resp.ID
	err = submit.containerCli.ContainerStart(context.TODO(), submit.containerID, types.ContainerStartOptions{})
	if err != nil {
		//fmtWriter(submit.errorBuffer, "3:%s\n", err)
		//passResultTCP(submit, BackendHostPort)
		submit.overallResult = 6
		serveResult(&overAllResult, submit, err.Error())
		return
	}

	defer containerStopAndRemove(submit)

	//get container IP address
	submit.containerInspect, _ = submit.containerCli.ContainerInspect(context.TODO(), submit.containerID)
	/*----------------------------------------------------------------------------*/
	containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8887")
	if err != nil {
		//fmtWriter(submit.errorBuffer, "%s\n", err)
		//passResultTCP(submit, BackendHostPort)
		submit.overallResult = 6
		serveResult(&overAllResult, submit, err.Error())
		return
	}

	var requests requestJSON
	requests.Command = "mkdir -p cafecoderUsers/" + submit.directoryName
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

	//tar copy
	usercodeFile, _ := os.Open("cafecoderUsers/" + submit.directoryName + "/" + submit.directoryName)
	content, err := ioutil.ReadAll(usercodeFile)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{
		Name: "cafecoderUsers/" + submit.directoryName + "/Main" + langExtention[submit.lang], // filename
		Mode: 0777,                                                                            // permissions
		Size: int64(len(content)),                                                             // filesize
	})
	tw.Write(content)
	tw.Close()
	submit.containerCli.CopyToContainer(
		context.TODO(), submit.containerID,
		"/",
		&buf, types.CopyToContainerOptions{},
	)
	usercodeFile.Close()
	println("checked")
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
		if _, exist := commandChickets.channel[session[1]]; exist { //if sessionID exists...
			fmt.Fprintf(os.Stdout, "%s has already existed\n", session[1])
		} else {
			commandChickets.channel[session[1]] = make(chan cmdResultJSON)
			go executeJudge(session, &tftpCli, &commandChickets.channel)
		}
	}
}
