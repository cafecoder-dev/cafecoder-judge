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
	"io/ioutil"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jinzhu/gorm"
	"pack.ag/tftp"
)

const (
	/*BackendHostPort ... backend's IP-address and port-number*/
	//BackendHostPort = "133.130.101.250:5963"
	BackendHostPort = "localhost:5963"
	maxTestcaseN    = 50
)

type cmdChicket struct {
	sync.Mutex
	channel map[string]chan cmdResultJSON
}

type cmdResultJSON struct {
	SessionID  string `json:"sessionID"`
	Time       int64  `json:"time"`
	Result     bool   `json:"result"`
	ErrMessage string `json:"errMessage"`
}

type requestJSON struct {
	SessionID string `json:"sessionID"`
	Cmd       string `json:"cmd"`
	Mode      string `json:"mode"` //Mode ... "judge" or "other"
	DirName   string `json:"dirName"`
}

type resultJSON struct {
	SessionID  string                     `json:"sessionID"`
	Time       int64                      `json:"time"`
	Result     string                     `json:"result"`
	Score      int                        `json:"score"`
	ErrMessage string                     `json:"errMessage"`
	TestcaseN  int                        `json:"testcaseN"` //
	Testcases  [maxTestcaseN]testcaseJSON `json:"testcases"`
}

type testcaseJSON struct {
	Name       string `json:"name"`
	Result     string `json:"result"`
	MemoryUsed int64  `json:"memory_used"`
	Time       int64  `json:"time"`
}

type submitGORM struct {
	Status       string `gorm:"column:status"`
	UsercodePath string `gorm:"column:usercodePath"`
	SessionID    string `gorm:"column:sessionID"`
	Language     string `gorm:"column:language"`
	TestcasePath string `gorm:"column:testcasePath"`
	ProblemID    string `gorm:"column:problemID"`
}

type submitT struct {
	sessionID       string
	usercodePath    string
	lang            string
	testcaseDirPath string

	dirName      string
	execDirPath  string
	execFilePath string
	fileName     string
	compileCmd   string
	executeCmd   string

	result resultJSON

	code             []byte
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

func manageCmds(cmdChickets *cmdChicket) {
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
			data, _ := base64.StdEncoding.DecodeString(cmdResult.ErrMessage)
			fmt.Println(string(data))
			cmdResult.ErrMessage = string(data)
			go func() {
				(*cmdChickets).channel[cmdResult.SessionID] <- cmdResult
			}()
		}()
	}
}

func sendResult(submit submitT) {
	priorityMap := map[string]int{"AC": 0, "WA": 1, "-": 2, "TLE": 3, "RE": 4, "MLE": 5, "CE": 6, "IE": 7}

	os.RemoveAll("cafecoderUsers/" + submit.dirName)
	fmt.Println("submit result")
	submit.result.SessionID = submit.sessionID
	if priorityMap[submit.result.Result] < 6 {
		submit.result.Result = "AC"
		for i := 0; i < submit.result.TestcaseN; i++ {
			fmt.Printf("i:%d %s\n", i, submit.result.Testcases[i].Result)
			if priorityMap[submit.result.Testcases[i].Result] > priorityMap[submit.result.Result] {
				submit.result.Result = submit.result.Testcases[i].Result
			}
			if submit.result.Testcases[i].Time > submit.result.Time {
				submit.result.Time = submit.result.Testcases[i].Time
			}
		}
	} else {
		submit.result.ErrMessage = base64.StdEncoding.EncodeToString([]byte(submit.errorBuffer.String()))
	}

	db, err := sqlConnect()
	if err != nil {
		fmt.Println(err.Error())
	}
	db.Table("users").Where("sessionID=?", submit.sessionID).Update("status", submit.result.Result)
}

func judge(args submitGORM, tftpCli **tftp.Client, cmdChickets *map[string]chan cmdResultJSON) {
	var submit = submitT{errorBuffer: new(bytes.Buffer), resultBuffer: new(bytes.Buffer)}

	submit.sessionID = args.SessionID

	errMessage := validationCheck(args)
	if errMessage != "" {
		fmt.Printf("%s\n", errMessage)
		submit.result.Result = "IE"
		sendResult(submit)
		return
	}

	submit.usercodePath = args.UsercodePath
	submit.lang = args.Language
	submit.testcaseDirPath = args.TestcasePath
	sessionIDChan := (*cmdChickets)[submit.sessionID]
	defer func() { delete((*cmdChickets), submit.sessionID) }()
	hash := sha256.Sum256([]byte(submit.sessionID))
	submit.dirName = hex.EncodeToString(hash[:])

	langConfig(&submit)

	/*todo: なんとかする*/
	//submit.code = tftpwrapper.DownloadFromPath(tftpCli, submit.usercodePath)
	submit.code, _ = ioutil.ReadFile(submit.usercodePath)

	os.Mkdir("cafecoderUsers/"+submit.dirName, 0777)
	file, _ := os.Create("cafecoderUsers/" + submit.dirName + "/" + submit.dirName)
	file.Write(submit.code)
	file.Close()

	err := createContainer(&submit)
	if err != nil {
		fmt.Printf("container:%s\n", err.Error())
		submit.result.Result = "IE"
		sendResult(submit)
		return
	}
	defer removeContainer(submit)

	err = tarCopy(
		"cafecoderUsers/"+submit.dirName+"/"+submit.dirName,
		submit.fileName,
		0777,
		submit,
	)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		submit.result.Result = "IE"
		sendResult(submit)
		return
	}

	err = compile(&submit, &sessionIDChan)
	if err != nil {
		submit.result.Result = "IE"
		sendResult(submit)
		return
	}
	if submit.result.Result == "CE" {
		sendResult(submit)
		return
	}

	err = tryTestcase(&submit, &sessionIDChan)
	if err != nil {
		submit.result.Result = "IE"
		sendResult(submit)
		return
	}
	println("test done")

	sendResult(submit)
	return
}

func compile(submit *submitT, sessionIDchan *chan cmdResultJSON) error {
	println("check")
	recv, err := requestCmd(submit.compileCmd, "other", *submit, sessionIDchan)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		return err
	}
	if !recv.Result {
		fmt.Printf("%s CE\n", recv.ErrMessage)
		submit.result.Result = "CE"
		return nil
	}
	println("compile done")
	return nil
}

func tryTestcase(submit *submitT, sessionIDChan *chan cmdResultJSON) error {
	var (
		TLEcase      bool
		testcaseName string
	)
	testcaseListFile, err := os.Open(submit.testcaseDirPath + "/testcase_list.txt")
	if err != nil {
		fmt.Printf("failed to open %s/testcase_list.txt\n", submit.testcaseDirPath)
		return err
	}
	sc := bufio.NewScanner(testcaseListFile)
	for sc.Scan() {
		submit.result.Testcases[submit.result.TestcaseN].Name = strings.TrimSpace(sc.Text())
		submit.result.TestcaseN++
	}
	testcaseListFile.Close()

	fmt.Printf("N=%d\n", submit.result.TestcaseN)
	for i := 0; i < submit.result.TestcaseN; i++ {
		if TLEcase {
			submit.result.Testcases[i].Result = "-"
			continue
		}
		testcaseName = submit.result.Testcases[i].Name
		outputTestcase, err := ioutil.ReadFile(submit.testcaseDirPath + "/out/" + testcaseName)
		if err != nil {
			println("readfile error")
			return err
		}
		err = tarCopy(
			submit.testcaseDirPath+"/in/"+testcaseName,
			"/testcase.txt",
			0744,
			*submit,
		)
		if err != nil {
			println("tar copy error")
			return err
		}

		recv, err := requestCmd(submit.executeCmd, "judge", *submit, sessionIDChan)
		if err != nil {
			println("requestCmd error")
			return err
		}

		stdoutBuf, err := copyFromContainer("userStdout.txt", *submit)
		if err != nil {
			println(err.Error())
			return err
		}
		stderrBuf, err := copyFromContainer("userStderr.txt", *submit)
		if err != nil {
			println(err.Error())
			return err
		}

		submit.result.Testcases[i].Time = recv.Time

		stdoutLines := strings.Split(stdoutBuf.String(), "\n")
		stderrLines := strings.Split(stderrBuf.String(), "\n")
		outputTestcaseLines := strings.Split(string(outputTestcase), "\n")

		if submit.result.Testcases[i].Time > 2000 {
			submit.result.Testcases[i].Result = "TLE"
			TLEcase = true
		} else {
			if !recv.Result && stdoutBuf.String() != "" {
				for j := 0; j < len(stderrLines); j++ {
					println(stderrLines[j])
				}
				submit.result.Testcases[i].Result = "RE"
			} else {
				submit.result.Testcases[i].Result = "WA"
				for j := 0; j < len(stdoutLines) && j < len(outputTestcaseLines); j++ {
					submit.result.Testcases[i].Result = "AC"
					if strings.TrimSpace(string(stdoutLines[j])) != strings.TrimSpace(string(outputTestcaseLines[j])) {
						submit.result.Testcases[i].Result = "WA"
						break
					}
				}
			}
		}
		//fmt.Printf("i:%d result:%s\n", i, submit.result.Testcases[i].Result)
	}
	return nil
}

func copyFromContainer(filepath string, submit submitT) (*bytes.Buffer, error) {
	var buffer *bytes.Buffer
	reader, _, err := submit.containerCli.CopyFromContainer(
		context.TODO(),
		submit.sessionID,
		filepath,
	)
	if err != nil {
		return buffer, err
	}
	defer reader.Close()
	tr := tar.NewReader(reader)
	tr.Next()
	buffer = new(bytes.Buffer)
	buffer.ReadFrom(tr)

	return buffer, nil
}

func tarCopy(hostFilePath string, containerFilePath string, mode int64, submit submitT) error {
	var buf bytes.Buffer
	usercodeFile, err := os.Open(hostFilePath)
	if err != nil {
		return err
	}
	content, err := ioutil.ReadAll(usercodeFile)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(&buf)
	err = tw.WriteHeader(
		&tar.Header{
			Name: containerFilePath,
			Mode: mode,
			Size: int64(len(content)),
		},
	)
	tw.Write(content)
	tw.Close()
	err = submit.containerCli.CopyToContainer(
		context.TODO(),
		submit.containerID,
		"/",
		&buf,
		types.CopyToContainerOptions{},
	)
	if err != nil {
		return err
	}
	usercodeFile.Close()
	fmt.Printf("copy to container done\n")
	return nil
}

func requestCmd(cmd string, mode string, submit submitT, sessionIDChan *chan cmdResultJSON) (cmdResultJSON, error) {
	var (
		request requestJSON
		recv    cmdResultJSON
	)
	containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8887")
	if err != nil {
		return recv, err
	}

	request.Cmd = cmd
	request.SessionID = submit.sessionID
	request.Mode = mode
	b, err := json.Marshal(request)
	if err != nil {
		return recv, err
	}
	fmt.Println(request)

	containerConn.Write(b)
	containerConn.Close()
	for {
		recv = <-*sessionIDChan
		if recv.SessionID == submit.sessionID {
			break
		}
	}
	fmt.Println(recv)
	return recv, nil
}

func removeContainer(submit submitT) {
	_ = submit.containerCli.ContainerStop(context.Background(), submit.containerID, nil)
	_ = submit.containerCli.ContainerRemove(context.Background(), submit.containerID, types.ContainerRemoveOptions{RemoveVolumes: true, RemoveLinks: true, Force: true})
	labelFilters := filters.NewArgs()
	submit.containerCli.ContainersPrune(context.Background(), labelFilters)
	fmt.Println("container " + submit.sessionID + " removed")
}

func createContainer(submit *submitT) error {
	var err error
	submit.containerCli, err = client.NewClientWithOpts(client.WithVersion("1.35"))
	defer submit.containerCli.Close()
	if err != nil {
		return err
	}

	config := &container.Config{Image: "cafecoder"}
	resp, err := submit.containerCli.ContainerCreate(context.TODO(), config, nil, nil, submit.sessionID)
	if err != nil {
		return err
	}

	submit.containerID = resp.ID
	err = submit.containerCli.ContainerStart(context.TODO(), submit.containerID, types.ContainerStartOptions{})
	if err != nil {
		return err
	}

	submit.containerInspect, err = submit.containerCli.ContainerInspect(context.TODO(), submit.containerID)
	if err != nil {
		return err
	}
	return nil
}

func langConfig(submit *submitT) {
	switch submit.lang {
	case "C11": //C11
		submit.compileCmd = "gcc Main.c -O2 -lm -std=gnu11 -o Main.out 2> userStderr.txt"
		submit.executeCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.c"
	case "C++17": //C++17
		submit.compileCmd = "g++ Main.cpp -O2 -lm -std=gnu++17 -o Main.out 2> userStderr.txt"
		submit.executeCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.cpp"
	case "Java8": //java8
		submit.compileCmd = "javac Main.java 2> userStderr.txt"
		submit.executeCmd = "java Main < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.java"
	case "Python3": //python3
		submit.compileCmd = "python3 -m py_compile Main.py 2> userStderr.txt"
		submit.executeCmd = "python3 Main.py < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.py"
	case "C#": //C#
		submit.compileCmd = "mcs Main.cs -out:Main.exe 2> userStderr.txt"
		submit.executeCmd = "mono Main.exe < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.cs"
	case "Go": //golang
		submit.compileCmd = "go build Main.go -o Main.out 2> userStderr.txt"
		submit.executeCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.go"
	}

}

func validationCheck(args submitGORM) string {
	if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, args.Language) {
		return "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'"
	}
	if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, args.ProblemID) {
		return "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'"
	}
	if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, args.SessionID) {
		return "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'"
	}
	if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, args.Status) {
		return "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'"
	}
	if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, args.TestcasePath) {
		return "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'"
	}
	if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, args.UsercodePath) {
		return "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'"
	}
	return ""
}

func sqlConnect() (database *gorm.DB, err error) {
	bytes, err := ioutil.ReadFile("pswd.txt")
	if err != nil {
		panic(err)
	}

	DBMS := "mysql"
	USER := "earlgray283"
	PASS := string(bytes)
	PROTOCOL := "tcp(localhost)"
	DBNAME := "cafecoder"

	CONNECT := USER + ":" + PASS + "@" + PROTOCOL + "/" + DBNAME + "?charset=utf8&parseTime=true&loc=Asia%2FTokyo"
	return gorm.Open(DBMS, CONNECT)
}

func main() {
	cmdChickets := cmdChicket{channel: make(map[string]chan cmdResultJSON)}
	go manageCmds(&cmdChickets)
	db, err := sqlConnect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
	tftpCli, err := tftp.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
	for {
		res := []submitGORM{}
		db.Table("users").Where("status='WR' OR status='WJ'").Find(&res)
		for i := 0; i < len(res); i++ {
			if res[i].Status == "" || res[i].SessionID == "" {
				println("NaaN")
				break
			}

			if _, exist := cmdChickets.channel[res[i].SessionID]; exist {
				//fmt.Printf("%s has already existed\n", res[i].SessionID)
				continue
			} else {
				fmt.Printf("id:%s status:%s\n", res[i].SessionID, res[i].Status)
				cmdChickets.channel[res[i].SessionID] = make(chan cmdResultJSON)
				go judge(res[i], &tftpCli, &cmdChickets.channel)
			}
		}
	}
}
