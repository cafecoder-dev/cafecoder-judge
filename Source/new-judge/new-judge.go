package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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

	execFilePath string

	testcaseN          int
	testcaseName       [100]string
	testcaseTime       [100]int64
	testcaseResult     [100]int
	testcaseMemoryUsed [100]int64

	compileCmd    string
	runcodeCmd    string
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

func languageCmdSet(submit *submitT) {
	switch submit.lang {
	case 0: //C11
		submit.compileCmd = "gcc" + " /cafecoderUsers/" + submit.sessionID + "/Main.c" + " -lm" + " -std=gnu11" + " -o" + " /cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.runcodeCmd = "timeout 3 ./cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.out"
	case 1: //C++17
		submit.compileCmd = "g++" + " /cafecoderUsers/" + submit.sessionID + "/Main.cpp" + " -lm" + " -std=gnu++17" + " -o" + " /cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.runcodeCmd = "timeout 3 ./cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.out"
	case 2: //java8
		submit.compileCmd = "javac" + " /cafecoderUsers/" + submit.sessionID + "/Main.java" + " -d" + " /cafecoderUsers/" + submit.sessionID
		submit.runcodeCmd = "timeout 3 java" + " -cp" + " /cafecoderUsers/" + submit.sessionID + " Main"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.class"
	case 3: //python3
		submit.compileCmd = "python3" + " -m" + " py_compile" + " /cafecoderUsers/" + submit.sessionID + "/Main.py"
		submit.runcodeCmd = "timeout 3 python3 /cafecoderUsers/" + submit.sessionID + "/Main.py"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.py"
	case 4: //C#
		submit.compileCmd = "mcs" + " /cafecoderUsers/" + submit.sessionID + "/Main.cs" + " -out:/cafecoderUsers/" + submit.sessionID + "/Main.exe"
		submit.runcodeCmd = "timeout 3 mono /cafecoderUsers/" + submit.sessionID + "/Main.exe"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.exe"
	case 5: //Ruby ... I don't know anything at all :(
		submit.compileCmd = "ruby" + " -cw" + " /cafecoderUsers/" + submit.sessionID + "/Main.rb"
		submit.runcodeCmd = "timeout 3 ./cafecoderUsers/" + submit.sessionID + "/Main.out"
		submit.execFilePath = "/cafecoderUsers/" + submit.sessionID + "/Main.rb"
	}
}

func compile(submit *submitT, sessionIDChan *chan cmdResultJSON) int {
	var err error

	recv, err := execCommandOnContainer(submit, *sessionIDChan, submit.compileCmd, "others")
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err.Error())
		return -1
	}
	if recv.Result == false {
		return -2
	}

	_, err = execCommandOnContainer(submit, *sessionIDChan, "chown rbash_user "+submit.execFilePath, "others")
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err.Error())
		return -1
	}

	return 0
}

func readFile(submit submitT, filePath string) (*bytes.Buffer, error) {
	var userBuffer *bytes.Buffer

	userReader, _, err := submit.containerCli.CopyFromContainer(context.TODO(), submit.sessionID, filePath)
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err)
		return userBuffer, err
	}
	tr := tar.NewReader(userReader)
	tr.Next()
	userBuffer = new(bytes.Buffer)
	userBuffer.ReadFrom(tr)

	return userBuffer, nil
}

func tryTestcase(submit *submitT, sessionIDChan *chan cmdResultJSON, overAllResult *overAllResultJSON) int {
	var testcaseName [256]string

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

		recv, err := execCommandOnContainer(submit, *sessionIDChan, submit.runcodeCmd, "judge")

		userStdout, err := readFile(*submit, "cafecoderUsers/"+submit.sessionID+"/userStdout.txt")
		if err != nil {
			return -1
		}
		userStderr, err := readFile(*submit, "cafecoderUsers/"+submit.sessionID+"/userStderr.txt")
		if err != nil {
			return -1
		}

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
		}
	}
	return 0
}

func serveResult(overAllResult *overAllResultJSON, submit submitT) {
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

func execCommandOnContainer(submit *submitT, sessionIDChan chan cmdResultJSON, cmd string, mode string) (cmdResultJSON, error) {
	var requests requestJSON
	var reserve cmdResultJSON
	containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8887")
	if err != nil {
		return reserve, err
	}
	requests.Command = cmd
	requests.SessionID = submit.sessionID
	requests.Mode = mode
	b, err := json.Marshal(requests)
	if err != nil {
		return reserve, err
	}
	println(string(b))
	containerConn.Write(b)
	containerConn.Close()
	println("waiting for " + cmd + "...")
	for {
		recv := <-sessionIDChan
		if submit.sessionID == recv.SessionID {
			fmtWriter(submit.errorBuffer, "%s\n", recv.ErrMessage)
			reserve = recv
			break
		}
	}
	return reserve, nil
}

//CreateAndStartContainer ...
func CreateAndStartContainer(submit *submitT) error {
	var err error

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

	return nil
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
		langExtention = [...]string{".c", ".cpp", ".java", ".py", ".cs", ".rb"}
		submit        = submitT{errorBuffer: new(bytes.Buffer), resultBuffer: new(bytes.Buffer)}
		err           error
		overAllResult overAllResultJSON
		buf           bytes.Buffer
	)
	initSubmit(&submit)

	submit.overallResult = 0
	if len(csv) > 1 {
		submit.sessionID = csv[1]
	}
	if len(csv) > 6 {
		submit.overallResult = 6
		fmtWriter(submit.errorBuffer, "too many args\n")
		serveResult(&overAllResult, submit)
		return
	}
	if len(csv) < 6 {
		submit.overallResult = 6
		fmtWriter(submit.errorBuffer, "too few args\n")
		serveResult(&overAllResult, submit)
		return
	}

	/*validation checks*/
	for i := range csv {
		if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, strings.TrimSpace(csv[i])) {
			fmtWriter(submit.errorBuffer, "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'\n")
			submit.overallResult = 6
			serveResult(&overAllResult, submit)
			return
		}
	}

	submit.usercodePath = csv[2]
	submit.lang, _ = strconv.Atoi(csv[3])
	submit.testcaseDirPath = csv[4]
	submit.score, _ = strconv.Atoi(csv[5])
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

	//about docker
	err = CreateAndStartContainer(&submit)
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err.Error())
		submit.overallResult = 6
		serveResult(&overAllResult, submit)
		return
	}
	defer submit.containerCli.Close()
	defer containerStopAndRemove(submit)

	_, err = execCommandOnContainer(&submit, sessionIDChan, "mkdir -p cafecoderUsers/"+submit.sessionID, "others")
	if err != nil {
		fmtWriter(submit.errorBuffer, "%s\n", err.Error())
		submit.overallResult = 6
		serveResult(&overAllResult, submit)
		return
	}

	//tar copy
	usercodeFile, _ := os.Open("cafecoderUsers/" + submit.sessionID + "/" + submit.sessionID)
	content, err := ioutil.ReadAll(usercodeFile)
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

	languageCmdSet(&submit)

	ret := compile(&submit, &sessionIDChan)
	if ret == -1 { //IE case...:(
		submit.overallResult = 6
		serveResult(&overAllResult, submit)
		return
	} else if ret == -2 { //CE case...
		submit.overallResult = 5
		serveResult(&overAllResult, submit)
		return
	}

	ret = tryTestcase(&submit, &sessionIDChan, &overAllResult)
	fmt.Println("tryTestcase() done")
	if ret == -1 {
		submit.overallResult = 6
		serveResult(&overAllResult, submit)
		return
	}
	serveResult(&overAllResult, submit)
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
