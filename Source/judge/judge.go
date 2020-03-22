package main

import (
	"archive/tar"
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
	"strconv"
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
	/*maxJudge ... Max number judge can execute at the same time*/
	maxJudge = 10
)

var now int

type cmdChicket struct {
	sync.Mutex
	channel map[string]chan cmdResultJSON
}

type cmdResultJSON struct {
	SessionID  string `json:"sessionID"`
	Time       int    `json:"time"`
	Result     bool   `json:"result"`
	ErrMessage string `json:"errMessage"`
	MemUsage   int    `json:"memUsage"`
}

type requestJSON struct {
	SessionID string `json:"sessionID"`
	Cmd       string `json:"cmd"`
	Mode      string `json:"mode"` //Mode ... "judge" or "other"
	DirName   string `json:"dirName"`
}

type resultGORM struct {
	Status          string `gorm:"column:status"`
	ExecutionTime   int    `gorm:"column:execution_time"`
	ExecutionMemory int    `gorm:"column:execution_memory"`
}

type testcaseResultsGORM struct {
	SubmitID        int64  `gorm:"column:submit_id"`
	TestcaseID      int64  `gorm:"column:testcase_id"`
	Status          string `gorm:"column:status"`
	ExecutionTime   int    `gorm:"column:execution_time"`
	ExecutionMemory int    `gorm:"column:execution_memory"`
}

type testcaseGORM struct {
	Input  string `gorm:"column:input"`
	Output string `gorm:"column:output"`
}

type submitGORM struct {
	ID        int64  `gorm:"column:id"`
	ProblemID int64  `gorm:"column:problem_id"`
	Path      string `gorm:"column:path"`
	Lang      string `gorm:"column:lang"`
}

type submitT struct {
	info            submitGORM
	result          resultGORM
	testcaseResults []testcaseResultsGORM

	dirName      string
	execDirPath  string
	execFilePath string
	fileName     string
	compileCmd   string
	executeCmd   string

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
	if priorityMap[submit.result.Status] < 6 {
		submit.result.Status = "AC"
		for i := 0; i < len(submit.testcaseResults); i++ {
			fmt.Printf("i:%d %s\n", i, submit.testcaseResults[i].Status)
			if priorityMap[submit.testcaseResults[i].Status] > priorityMap[submit.result.Status] {
				submit.result.Status = submit.testcaseResults[i].Status
			}
			if submit.testcaseResults[i].ExecutionTime > submit.result.ExecutionTime {
				submit.result.ExecutionTime = submit.testcaseResults[i].ExecutionTime
			}
		}
	} else {
		//submit.result.ErrMessage = base64.StdEncoding.EncodeToString([]byte(submit.errorBuffer.String()))
	}

	db, err := sqlConnect()
	if err != nil {
		fmt.Println(err.Error())
	}
	db.
		Table("submits").
		Where("sessionID=? AND WHERE deleted_at IS NULL", strconv.FormatInt(submit.info.ID, 10)).
		Update(&submit.result)

	now--
}

func judge(args submitGORM, tftpCli **tftp.Client, cmdChickets *map[string]chan cmdResultJSON) {
	var submit = submitT{errorBuffer: new(bytes.Buffer), resultBuffer: new(bytes.Buffer)}

	errMessage := validationCheck(args)
	if errMessage != "" {
		fmt.Printf("%s\n", errMessage)
		submit.result.Status = "IE"
		sendResult(submit)
		return
	}

	submit.info = args
	sessionIDChan := (*cmdChickets)[strconv.FormatInt(submit.info.ID, 10)]
	defer func() { delete((*cmdChickets), strconv.FormatInt(submit.info.ID, 10)) }()
	hash := sha256.Sum256([]byte(strconv.FormatInt(submit.info.ID, 10)))
	submit.dirName = hex.EncodeToString(hash[:])

	langConfig(&submit)

	/*todo: なんとかする*/
	//submit.code = tftpwrapper.DownloadFromPath(tftpCli, submit.usercodePath)
	submit.code, _ = ioutil.ReadFile(submit.info.Path)

	os.Mkdir("cafecoderUsers/"+submit.dirName, 0777)
	file, _ := os.Create("cafecoderUsers/" + submit.dirName + "/" + submit.dirName)
	file.Write(submit.code)
	file.Close()

	err := createContainer(&submit)
	if err != nil {
		fmt.Printf("container:%s\n", err.Error())
		submit.result.Status = "IE"
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
		submit.result.Status = "IE"
		sendResult(submit)
		return
	}

	err = compile(&submit, &sessionIDChan)
	if err != nil {
		submit.result.Status = "IE"
		sendResult(submit)
		return
	}
	if submit.result.Status == "CE" {
		sendResult(submit)
		return
	}

	err = tryTestcase(&submit, &sessionIDChan)
	if err != nil {
		submit.result.Status = "IE"
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
		submit.result.Status = "CE"
		return nil
	}
	println("compile done")
	return nil
}

func tryTestcase(submit *submitT, sessionIDChan *chan cmdResultJSON) error {
	var (
		TLEcase bool
	)
	db, err := sqlConnect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
	testcases := []testcaseGORM{}
	db.
		Table("testcases").
		Where("problem_id=? deleted_at IS NULL", strconv.FormatInt(submit.info.ProblemID, 10)).
		Find(&testcases)

	for i := 0; i < len(testcases); i++ {
		if TLEcase {
			submit.testcaseResults[i].Status = "-"
			continue
		}

		file, err := os.Create("cafecoderUsers/" + submit.dirName + "/testcase.txt")
		if err != nil {
			fmt.Println(err)
		}
		file.Write(([]byte)(testcases[i].Input))
		file.Close()

		err = tarCopy(
			"cafecoderUsers/"+submit.dirName+"/testcase.txt",
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

		submit.testcaseResults[i].ExecutionTime = recv.Time
		submit.testcaseResults[i].ExecutionTime = recv.MemUsage

		stdoutLines := strings.Split(stdoutBuf.String(), "\n")
		stderrLines := strings.Split(stderrBuf.String(), "\n")
		outputTestcaseLines := strings.Split(string(testcases[i].Output), "\n")

		if recv.Time > 2000 {
			submit.testcaseResults[i].Status = "TLE"
			TLEcase = true
		} else {
			if !recv.Result && stdoutBuf.String() != "" {
				for j := 0; j < len(stderrLines); j++ {
					println(stderrLines[j])
				}
				submit.testcaseResults[i].Status = "RE"
			} else {
				submit.testcaseResults[i].Status = "WA"
				for j := 0; j < len(stdoutLines) && j < len(outputTestcaseLines); j++ {
					submit.testcaseResults[i].Status = "AC"
					if strings.TrimSpace(string(stdoutLines[j])) != strings.TrimSpace(string(outputTestcaseLines[j])) {
						submit.testcaseResults[i].Status = "WA"
						break
					}
				}
			}
		}
		db.
			Table("testcase_results").
			Where("problem_id=? AND deleted_at IS NULL", strconv.FormatInt(submit.info.ProblemID, 10)).
			Create(&submit.testcaseResults)

		//fmt.Printf("i:%d result:%s\n", i, submit.result.Testcases[i].Result)
	}
	return nil
}

func copyFromContainer(filepath string, submit submitT) (*bytes.Buffer, error) {
	var buffer *bytes.Buffer
	reader, _, err := submit.containerCli.CopyFromContainer(
		context.TODO(),
		strconv.FormatInt(submit.info.ID, 10),
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
	request.SessionID = strconv.FormatInt(submit.info.ID, 10)
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
		if recv.SessionID == strconv.FormatInt(submit.info.ID, 10) {
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
	fmt.Println("container " + strconv.FormatInt(submit.info.ID, 10) + " removed")
}

func createContainer(submit *submitT) error {
	var err error
	submit.containerCli, err = client.NewClientWithOpts(client.WithVersion("1.35"))
	defer submit.containerCli.Close()
	if err != nil {
		return err
	}

	config := &container.Config{Image: "cafecoder"}
	resp, err := submit.containerCli.ContainerCreate(context.TODO(), config, nil, nil, strconv.FormatInt(submit.info.ID, 10))
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
	switch submit.info.Lang {
	case "c17_gcc9": //C11
		submit.compileCmd = "gcc-9 Main.c -O2 -lm -std=gnu17 -o Main.out 2> userStderr.txt"
		submit.executeCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.c"
	case "cpp17_gcc9": //C++17
		submit.compileCmd = "g++-9 Main.cpp -O2 -lm -std=gnu++17 -o Main.out 2> userStderr.txt"
		submit.executeCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.cpp"
	case "cpp20_gcc9": //C++17
		submit.compileCmd = "g++-9 Main.cpp -O2 -lm -std=gnu++2a -o Main.out 2> userStderr.txt"
		submit.executeCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.cpp"
	case "java11": //java8
		submit.compileCmd = "javac Main.java 2> userStderr.txt"
		submit.executeCmd = "java Main < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.java"
	case "python36": //python3
		submit.compileCmd = "python3 -m py_compile Main.py 2> userStderr.txt"
		submit.executeCmd = "python3 Main.py < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.py"
	case "cs_mono6": //C#
		submit.compileCmd = "mcs Main.cs -out:Main.exe 2> userStderr.txt"
		submit.executeCmd = "mono Main.exe < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.cs"
	case "cs_dotnet31":
		submit.compileCmd = "mkdir Main && mv Main.cs Main && cd Main && dotnet new console 2> ../userStderr.txt && dotnet publish -o . > ../userStderr.txt"
		submit.executeCmd = "dotnet ./Main/Main.dll > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.cs"
	case "go_114": //golang
		submit.compileCmd = "export PATH=$PATH:/usr/local/go/bin && mkdir Main && mv Main.go Main && cd Main && go build . 2> ../userStderr.txt"
		submit.executeCmd = "./Main/Main < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.go"
	case "nim":
		submit.compileCmd = "nim cpp -d:release --opt:speed --multimethods:on -o:Main.out Main.nim 2> userStderr.txt"
		submit.executeCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.nim"
	case "rust_114":
		submit.compileCmd = "export PATH=\"$HOME/.cargo/bin:$PATH\" 2> userStderr.txt && rustc -O -o Main.out Main.rs 2> userStderr.txt"
		submit.executeCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.rs"
	}
}

func validationCheck(args submitGORM) string {
	if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, args.Lang) {
		return "Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'"
	}
	if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, args.Path) {
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
		db.Table("users").Where("status='WR' OR status='WJ'").Order("updated_at").Find(&res)
		for i := 0; i < len(res); i++ {
			if _, exist := cmdChickets.channel[strconv.FormatInt(res[i].ID, 10)]; exist {
				//fmt.Printf("%s has already existed\n", res[i].SessionID)
				continue
			} else {
				// wait untill ${maxJudge} isn't equal to ${now}
				for now == maxJudge {
				}
				now++
				fmt.Printf("id:%lld now:%d\n", res[i].ID, now)
				cmdChickets.channel[strconv.FormatInt(res[i].ID, 10)] = make(chan cmdResultJSON)
				go judge(res[i], &tftpCli, &cmdChickets.channel)
			}
		}
	}
}
