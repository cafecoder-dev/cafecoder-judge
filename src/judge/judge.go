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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jinzhu/gorm"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
	"pack.ag/tftp"
)

const (
	maxTestcaseN = 50
	maxJudge     = 20
)

// judgeNumberLimit limits the number of judges
//
// see: https://mattn.kaoriya.net/software/lang/go/20171221111857.htm
var judgeNumberLimit = make(chan struct{}, maxJudge)

type cmdTicket struct {
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
	CreatedAt       string `gorm:"colum:created_at"`
	UpdatedAt       string `gorm:"column:updated_at"`
}

type testcaseGORM struct {
	TestcaseID int64  `gorm:"column:id"`
	Input      string `gorm:"column:input"`
	Output     string `gorm:"column:output"`
}

type submitGORM struct {
	ID        int64  `gorm:"column:id"`
	Status    string `gorm:"status"`
	ProblemID int64  `gorm:"column:problem_id"`
	Path      string `gorm:"column:path"`
	Lang      string `gorm:"column:lang"`
}

type submitT struct {
	info            submitGORM
	result          resultGORM
	testcaseResults [128]testcaseResultsGORM

	hashedID     string
	execDirPath  string
	execFilePath string
	fileName     string
	compileCmd   string
	executeCmd   string

	codePath         string
	containerCli     *client.Client
	containerID      string
	containerInspect types.ContainerJSON

	resultBuffer *bytes.Buffer
	errorBuffer  *bytes.Buffer
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

func checkRegexp(reg, str string) bool {
	return regexp.MustCompile(reg).Match([]byte(str))
}

func timeToString(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func manageCmds(cmdChickets *cmdTicket) {
	listen, err := net.Listen("tcp", "0.0.0.0:3344")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s\n", err)
	}

	for {
		cnct, err := listen.Accept()
		if err != nil {
			continue //continue to receive request
		}
		go func() {
			var cmdResult cmdResultJSON
			_ = json.NewDecoder(cnct).Decode(&cmdResult)
			_ = cnct.Close()
			println("connection closed")
			data, _ := base64.StdEncoding.DecodeString(cmdResult.ErrMessage)
			fmt.Println(string(data))
			cmdResult.ErrMessage = string(data)
			go func() {
				(*cmdChickets).Lock()
				(*cmdChickets).channel[cmdResult.SessionID] <- cmdResult
				(*cmdChickets).Unlock()
			}()
		}()
	}
}

func sendResult(submit submitT) {
	priorityMap := map[string]int{"AC": 0, "WA": 1, "-": 2, "TLE": 3, "RE": 4, "MLE": 5, "CE": 6, "IE": 7}

	os.Remove(submit.codePath)
	os.Remove("./codes/" + submit.hashedID)

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
			if submit.testcaseResults[i].ExecutionMemory > submit.result.ExecutionMemory {
				submit.result.ExecutionMemory = submit.testcaseResults[i].ExecutionMemory
			}
		}
	}

	db, err := sqlConnect()
	if err != nil {
		fmt.Println(err.Error())
	}
	defer db.Close()

	db.
		Table("submits").
		Where("id=? AND deleted_at IS NULL", submit.info.ID).
		Update(&submit.result)

	<-judgeNumberLimit
}

func judge(args submitGORM, tftpCli **tftp.Client, cmdChickets *cmdTicket) {
	var submit = submitT{errorBuffer: new(bytes.Buffer), resultBuffer: new(bytes.Buffer)}

	errMessage := validationCheck(args)
	if errMessage != "" {
		fmt.Printf("%s\n", errMessage)
		submit.result.Status = "IE"
		sendResult(submit)
		return
	}

	submit.info = args
	id := fmt.Sprintf("%d", submit.info.ID) // submit.info.ID を文字列に変換

	(*cmdChickets).Lock()
	sessionIDChan := (*cmdChickets).channel[id]
	(*cmdChickets).Unlock()

	defer func() {
		(*cmdChickets).Lock()
		delete((*cmdChickets).channel, id)
		(*cmdChickets).Unlock()
	}()

	// dir name はハッシュです
	hash := sha256.Sum256([]byte(id))
	submit.hashedID = hex.EncodeToString(hash[:])

	if err := langConfig(&submit); err != nil {
		println(err.Error())
		submit.result.Status = "IE"
		sendResult(submit)
		return
	}

	codePath, err := saveSourceCode(submit)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		submit.result.Status = "IE"
		sendResult(submit)
		return
	}

	submit.codePath = codePath

	err = createContainer(&submit)
	if err != nil {
		fmt.Printf("[ERROR] container: %s\n", err.Error())
		submit.result.Status = "IE"
		sendResult(submit)
		return
	}
	defer removeContainer(submit)

	if err := tarCopy(codePath, submit.fileName, 0777, submit); err != nil {
		fmt.Printf("%s\n", err.Error())
		submit.result.Status = "IE"
		sendResult(submit)
		return
	}
	println("tar copy done")

	if err = compile(&submit, &sessionIDChan); err != nil {
		submit.result.Status = "IE"
		sendResult(submit)
		return
	}
	println("conpile done")

	if submit.result.Status == "CE" {
		sendResult(submit)
		return
	}

	if err = tryTestcase(&submit, &sessionIDChan); err != nil {
		submit.result.Status = "IE"
		sendResult(submit)
		return
	}
	println("judge done")

	sendResult(submit)

	return
}

func compile(submit *submitT, sessionIDchan *chan cmdResultJSON) error {
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
	defer db.Close()

	testcases := []testcaseGORM{}
	db.
		Table("testcases").
		Where("problem_id=? AND deleted_at IS NULL", strconv.FormatInt(submit.info.ProblemID, 10)).
		Find(&testcases)

	for i := 0; i < len(testcases); i++ {
		// skip
		if TLEcase {
			submit.testcaseResults[i].Status = "-"
			continue
		}

		file, _ := os.Create("./codes/" + submit.hashedID)

		file.Write(([]byte)(testcases[i].Input))
		file.Close()

		if err = tarCopy("./codes/"+submit.hashedID, "/testcase.txt", 0744, *submit); err != nil {
			println("tar copy error")
			return err
		}

		recv, err := requestCmd(submit.executeCmd, "judge", *submit, sessionIDChan)
		if err != nil {
			println("requestCmd error")
			return err
		}

		stdoutBuf, err := copyFromContainer("/userStdout.txt", *submit)
		if err != nil {
			println(err.Error())
			return err
		}
		stderrBuf, err := copyFromContainer("/userStderr.txt", *submit)
		if err != nil {
			println(err.Error())
			return err
		}

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

		submit.testcaseResults[i].TestcaseID = testcases[i].TestcaseID
		submit.testcaseResults[i].ExecutionTime = recv.Time
		submit.testcaseResults[i].ExecutionMemory = recv.MemUsage
		submit.testcaseResults[i].SubmitID = submit.info.ID
		submit.testcaseResults[i].CreatedAt = timeToString(time.Now())
		submit.testcaseResults[i].UpdatedAt = timeToString(time.Now())
	}

	for i := 0; i < len(testcases); i++ {
		if submit.info.Status == "WR" {
			db.
				Where("submit_id = ? AND testcase_id = ?", submit.info.ID, testcases[i].TestcaseID).
				Update(&submit.testcaseResults[i].UpdatedAt)
		} else if submit.info.Status == "WJ" {
			db.
				Table("testcase_results").
				Create(&submit.testcaseResults[i])
		}
	}

	return nil
}

func copyFromContainer(filepath string, submit submitT) (*bytes.Buffer, error) {
	var buffer *bytes.Buffer
	reader, _, err := submit.containerCli.CopyFromContainer(
		context.TODO(),
		submit.containerID,
		filepath,
	)
	if err != nil {
		return buffer, err
	}

	defer reader.Close()

	tr := tar.NewReader(reader)
	_, _ = tr.Next()
	buffer = new(bytes.Buffer)
	_, _ = buffer.ReadFrom(tr)

	return buffer, nil
}

// コンテナにコードコピーしてる
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

	_, _ = tw.Write(content)
	_ = tw.Close()
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
	_ = usercodeFile.Close()
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
	request.SessionID = fmt.Sprintf("%d", submit.info.ID)
	request.Mode = mode
	b, err := json.Marshal(request)
	if err != nil {
		return recv, err
	}
	fmt.Println(request)

	_, _ = containerConn.Write(b)
	_ = containerConn.Close()
	for {
		recv = <-*sessionIDChan
		if recv.SessionID == fmt.Sprintf("%d", submit.info.ID) {
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
	_, _ = submit.containerCli.ContainersPrune(context.Background(), labelFilters)
	fmt.Println("container: " + submit.containerID + " removed")
}

func createContainer(submit *submitT) error {
	var err error
	submit.containerCli, err = client.NewClientWithOpts(client.WithVersion("1.35"))
	defer submit.containerCli.Close()
	if err != nil {
		return err
	}

	config := &container.Config{Image: "cafecoder"}

	resp, err := submit.containerCli.ContainerCreate(context.TODO(), config, nil, nil, nil, submit.hashedID)
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

func saveSourceCode(submit submitT) (string, error) {
	credentialFilePath := "./key.json"

	ctx := context.Background()
	client, err := storage.NewClient(ctx, option.WithCredentialsFile(credentialFilePath))
	if err != nil {
		return "", err
	}

	var fileName = strings.Split(submit.info.Path, "/")[1]
	savePath := "./codes/" + fileName
	fp, err := os.Create(savePath)
	if err != nil {
		return "", err
	}

	bucket := "cafecoder-submit-source"
	obj := client.Bucket(bucket).Object(submit.info.Path)
	reader, err := obj.NewReader(ctx)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	tee := io.TeeReader(reader, fp)
	s := bufio.NewScanner(tee)
	for s.Scan() {
	}
	if err := s.Err(); err != nil {
		return "", err
	}

	println("save done id: " + fmt.Sprintf("%d", submit.info.ID))

	return savePath, nil
}

func langConfig(submit *submitT) error {
	var err error
	err = nil
	switch submit.info.Lang {
	case "c17_gcc10": //C11
		submit.compileCmd = "gcc-10 Main.c -O2 -lm -std=gnu17 -o Main.out 2> userStderr.txt"
		submit.executeCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.c"
	case "cpp17_gcc10": //C++17
		submit.compileCmd = "g++-10 Main.cpp -O2 -lm -std=gnu++17 -o Main.out 2> userStderr.txt"
		submit.executeCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.cpp"
	case "cpp20_gcc10": //C++17
		submit.compileCmd = "g++-10 Main.cpp -O2 -lm -std=gnu++2a -o Main.out 2> userStderr.txt"
		submit.executeCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.cpp"
	case "java11": //java8
		submit.compileCmd = "javac Main.java 2> userStderr.txt"
		submit.executeCmd = "java Main < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.java"
	case "python38": //python3
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
	case "rust_115":
		submit.compileCmd = "export PATH=\"$HOME/.cargo/bin:$PATH\" 2> userStderr.txt && rustc -O -o Main.out Main.rs 2> userStderr.txt"
		submit.executeCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.fileName = "Main.rs"
	default:
		err = errors.New("undefined language")
	}

	return err
}

func sqlConnect() (database *gorm.DB, err error) {
	if err = godotenv.Load("./.env"); err != nil {
		println("?")
		return nil, err
	}

	DBMS := "mysql"
	DBNAME := "p6jav_cafecoder_prod"
	USER := os.Getenv("DB_USER")
	PASS := os.Getenv("DB_PASS")
	HOST := os.Getenv("DB_HOST")
	PORT := os.Getenv("DB_PORT")

	PROTOCOL := fmt.Sprintf("tcp(%s:%s)", HOST, PORT)

	CONNECT := USER + ":" + PASS + "@" + PROTOCOL + "/" + DBNAME + "?charset=utf8&parseTime=true&loc=Asia%2FTokyo"

	return gorm.Open(DBMS, CONNECT)
}

func main() {
	cmdChickets := cmdTicket{channel: make(map[string]chan cmdResultJSON)}

	go manageCmds(&cmdChickets)

	db, err := sqlConnect()
	if err != nil {
		log.Fatal(err)
	}

	tftpCli, err := tftp.NewClient()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s\n", err)
	}

	for {
		var res []submitGORM
		db.Table("submits").Where("status='WR' OR status='WJ'").Order("updated_at").Find(&res)
		for i := 0; i < len(res); i++ {
			cmdChickets.Lock()
			_, exist := cmdChickets.channel[fmt.Sprintf("%d", res[i].ID)]
			cmdChickets.Unlock()

			if exist {
				continue
			} else {
				// wait until the number of judges becomes less than maxJudge
				judgeNumberLimit <- struct{}{}

				// fmt.Printf("id:%d now:%d\n", res[i].ID, now)

				cmdChickets.Lock()
				cmdChickets.channel[strconv.FormatInt(res[i].ID, 10)] = make(chan cmdResultJSON)
				cmdChickets.Unlock()

				go judge(res[i], &tftpCli, &cmdChickets)
			}
		}
	}
}
