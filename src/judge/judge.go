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
)

const (
	maxJudge = 20
	apiVersion = "1.40"
)

// judgeNumberLimit limits the number of judges
//
// see: https://mattn.kaoriya.net/software/lang/go/20171221111857.htm
var judgeNumberLimit = make(chan struct{}, maxJudge)

type CmdTicket struct {
	sync.Mutex
	channel map[string]chan CmdResultJSON
}

type CmdResultJSON struct {
	SessionID  string `json:"sessionID"`
	Time       int    `json:"time"`
	Result     bool   `json:"result"`
	ErrMessage string `json:"errMessage"`
	MemUsage   int    `json:"memUsage"`
}

type RequestJSON struct {
	SessionID string `json:"sessionID"`
	Cmd       string `json:"cmd"`
	Mode      string `json:"mode"` //Mode ... "judge" or "other"
	DirName   string `json:"dirName"`
}

type ResultGORM struct {
	Status          string `gorm:"column:status"`
	ExecutionTime   int    `gorm:"column:execution_time"`
	ExecutionMemory int    `gorm:"column:execution_memory"`
	Point           int    `gorm:"column:point"` // int64 にしたほうがいいかもしれない(カラムにあわせて int にした)
}

type TestcaseResultsGORM struct {
	SubmitID        int64  `gorm:"column:submit_id"`
	TestcaseID      int64  `gorm:"column:testcase_id"`
	Status          string `gorm:"column:status"`
	ExecutionTime   int    `gorm:"column:execution_time"`
	ExecutionMemory int    `gorm:"column:execution_memory"`
	CreatedAt       string `gorm:"column:created_at"`
	UpdatedAt       string `gorm:"column:updated_at"`
}

type TestcaseGORM struct {
	TestcaseID int64  `gorm:"column:id"`
	Input      string `gorm:"column:input"`
	Output     string `gorm:"column:output"`
}

type SubmitGORM struct {
	ID        int64  `gorm:"column:id"`
	Status    string `gorm:"column:status"`
	ProblemID int64  `gorm:"column:problem_id"`
	Path      string `gorm:"column:path"`
	Lang      string `gorm:"column:lang"`
}

type TestcaseSetsGORM struct {
	ID     int64 `gorm:"column:id"`
	Points int   `gorm:"column:points"`
}

type TestcaseTestcaseSetsGORM struct {
	TestcaseID    int64 `gorm:"column:testcase_id"`
	TestcaseSetID int64 `gorm:"column:testcase_set_id"`
}

type SubmitT struct {
	info   SubmitGORM
	result ResultGORM

	firstIndex         int64
	testcases          []TestcaseGORM
	testcaseResultsMap map[int64]TestcaseResultsGORM

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
}

func validationCheck(args SubmitGORM) bool {
	if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, args.Lang) || !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, args.Path) {
		return false
	}
	return true
	//"Inputs are included another characters[0-9],[a-z],[A-Z],'.','/','_'"
}

func checkRegexp(reg, str string) bool {
	compiled, err := regexp.Compile(reg)
	if err != nil {
		fmt.Println(err)
		return false
	}

	return compiled.Match([]byte(str))
}

func timeToString(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// コンテナからの応答を待つ。@Nperair さんが作りました
func manageCmds(cmdChickets *CmdTicket) {
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
			var cmdResult CmdResultJSON
			_ = json.NewDecoder(cnct).Decode(&cmdResult)
			_ = cnct.Close()
			println("connection closed")
			data, _ := base64.StdEncoding.DecodeString(cmdResult.ErrMessage)

			cmdResult.ErrMessage = string(data)
			go func() {
				(*cmdChickets).Lock()
				(*cmdChickets).channel[cmdResult.SessionID] <- cmdResult
				(*cmdChickets).Unlock()
			}()
		}()
	}
}

// 最終的な結果を DB に投げる。モジュールの分割が雑すぎるからなんとかしたい
func sendResult(submit SubmitT) {
	priorityMap := map[string]int{"-": 0, "AC": 1, "WA": 2, "TLE": 3, "RE": 4, "MLE": 5, "CE": 6, "IE": 7}

	if priorityMap[submit.result.Status] < 6 {
		submit.result.Status = "AC"
		for _, elem := range submit.testcaseResultsMap {
			if priorityMap[elem.Status] > priorityMap[submit.result.Status] {
				submit.result.Status = elem.Status
			}
			if elem.ExecutionTime > submit.result.ExecutionTime {
				submit.result.ExecutionTime = elem.ExecutionTime
			}
			if elem.ExecutionMemory > submit.result.ExecutionMemory {
				submit.result.ExecutionMemory = elem.ExecutionMemory
			}
		}
	}

	db, err := sqlConnect()
	if err != nil {
		fmt.Println(err.Error())
	}
	defer db.Close()


	for _, elem := range submit.testcaseResultsMap {
		if submit.info.Status == "WR" {
			db.
				Table("testcase_results").
				Where("submit_id = ? AND testcase_id = ?", submit.info.ID, elem.TestcaseID).
				Update(elem.UpdatedAt).
				Update(elem.Status).
				Update(elem.ExecutionTime).
				Update(elem.ExecutionMemory)
		} else if submit.info.Status == "WJ" {
			db.
				Table("testcase_results").
				Create(&elem)
		}
	}

	submit.result.Point = int(scoring(submit))

	db.
		Table("submits").
		Where("id=? AND deleted_at IS NULL", submit.info.ID).
		Update(&submit.result).
		Update("point", submit.result.Point).
		Update("execution_memory", submit.result.ExecutionMemory)

	<-judgeNumberLimit
}

// テストケースセットからスコアリング
func scoring(submit SubmitT) int64 {
	// テストケースごとの結果を得られない
	if submit.result.Status == "IE" || submit.result.Status == "CE" {
		return 0
	}

	db, err := sqlConnect()
	if err != nil {
		fmt.Println(err.Error())
	}
	defer db.Close()

	var (
		testcaseSets         []TestcaseSetsGORM
		testcaseTestcaseSets []TestcaseTestcaseSetsGORM
	)

	db.
		Table("testcase_sets").
		Where("problem_id=?", submit.info.ProblemID).
		Find(&testcaseSets)
	db.
		Table("testcase_testcase_sets").
		Joins("INNER JOIN testcases ON testcase_testcase_sets.testcase_id = testcases.id").
		Where("problem_id=? AND testcase_testcase_sets.deleted_at IS NULL", submit.info.ProblemID).
		Find(&testcaseTestcaseSets)

	// testcase_set_id -> testcase_id
	testcaseSetMap := map[int64][]int64{}

	for _, testcaseSet := range testcaseSets {
		testcaseSetMap[testcaseSet.ID] = make([]int64, 0)
	}
	for _, testcaseTestcaseSet := range testcaseTestcaseSets {
		testcaseSetMap[testcaseTestcaseSet.TestcaseSetID] =
			append(testcaseSetMap[testcaseTestcaseSet.TestcaseSetID], testcaseTestcaseSet.TestcaseID)
	}

	score := int64(0)
	for _, testcaseSet := range testcaseSets {
		isAC := true

		for _, testcaseID := range testcaseSetMap[testcaseSet.ID] {
			if submit.testcaseResultsMap[testcaseID].Status != "AC" {
				fmt.Printf("status(%d): %s\n",testcaseID, submit.testcaseResultsMap[testcaseID].Status)
				isAC = false
				break
			}
		}

		if isAC {
			score += int64(testcaseSet.Points)
		}
	}

	return score
}

// ジャッジのフロー　tryTestcase と混同するけど致し方ない・・？
func judge(args SubmitGORM, cmdChickets *CmdTicket) {
	var submit = SubmitT{}
	submit.testcaseResultsMap = map[int64]TestcaseResultsGORM{}

	if !validationCheck(args) {
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

	submit.hashedID = makeStringHash(id)

	defer func() {
		os.Remove(submit.codePath)
		os.Remove("./codes/" + submit.hashedID)
	}()

	if err := langConfig(&submit); err != nil {
		println(err.Error())
		submit.result.Status = "IE"
		sendResult(submit)
		return
	}

	codePath, err := downloadSourceCode(submit)
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

func compile(submit *SubmitT, sessionIDchan *chan CmdResultJSON) error {
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

func makeStringHash(str string) string {
	hash := sha256.Sum256([]byte(str))
	return hex.EncodeToString(hash[:])
}

func tryTestcase(submit *SubmitT, sessionIDChan *chan CmdResultJSON) error {
	var (
		TLEcase bool
	)

	db, err := sqlConnect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
	defer db.Close()

	testcases := []TestcaseGORM{}
	var testcasesNum = 0
	db.
		Table("testcases").
		Where("problem_id=? AND deleted_at IS NULL", strconv.FormatInt(submit.info.ProblemID, 10)).
		Order("id").
		Find(&testcases).
		Count(&testcasesNum)

	submit.testcases = testcases

	for i := 0; i < testcasesNum; i++ {
		testcaseResults := TestcaseResultsGORM{SubmitID: submit.info.ID, TestcaseID: submit.testcases[i].TestcaseID}

		// skip
		if TLEcase {
			testcaseResults.Status = "-"
			testcaseResults.CreatedAt = timeToString(time.Now())
			testcaseResults.UpdatedAt = timeToString(time.Now())
			submit.testcaseResultsMap[submit.testcases[i].TestcaseID] = testcaseResults
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
			testcaseResults.Status = "TLE"
			TLEcase = true
		} else {
			if !recv.Result {
				for j := 0; j < len(stderrLines); j++ {
					println(stderrLines[j])
				}
				testcaseResults.Status = "RE"
			} else {
				testcaseResults.Status = "WA"
				for j := 0; j < len(stdoutLines) && j < len(outputTestcaseLines); j++ {
					testcaseResults.Status = "AC"
					if strings.TrimSpace(string(stdoutLines[j])) != strings.TrimSpace(string(outputTestcaseLines[j])) {
						testcaseResults.Status = "WA"
						break
					}
				}
			}
		}

		testcaseResults.ExecutionTime = recv.Time
		testcaseResults.ExecutionMemory = recv.MemUsage
		if submit.info.Status == "WR" {
			testcaseResults.CreatedAt = timeToString(time.Now())
		}
		testcaseResults.UpdatedAt = timeToString(time.Now())

		submit.testcaseResultsMap[testcaseResults.TestcaseID] = testcaseResults
	}

	return nil
}

func copyFromContainer(filepath string, submit SubmitT) (*bytes.Buffer, error) {
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
func tarCopy(hostFilePath string, containerFilePath string, mode int64, submit SubmitT) error {
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

// 起動中のコンテナにコマンドをリクエストする
func requestCmd(cmd string, mode string, submit SubmitT, sessionIDChan *chan CmdResultJSON) (CmdResultJSON, error) {
	var (
		request RequestJSON
		recv    CmdResultJSON
	)

	containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8887")
	if err != nil {
		return recv, err
	}

	request = RequestJSON{Cmd: cmd, SessionID: fmt.Sprintf("%d", submit.info.ID), Mode: mode}
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

func removeContainer(submit SubmitT) {
	_ = submit.containerCli.ContainerStop(context.Background(), submit.containerID, nil)
	_ = submit.containerCli.ContainerRemove(context.Background(), submit.containerID, types.ContainerRemoveOptions{RemoveVolumes: true, RemoveLinks: true, Force: true})
	labelFilters := filters.NewArgs()
	_, _ = submit.containerCli.ContainersPrune(context.Background(), labelFilters)
	fmt.Println("container: " + submit.containerID + " removed")
}

func createContainer(submit *SubmitT) error {
	var err error
	submit.containerCli, err = client.NewClientWithOpts(client.WithVersion(apiVersion))
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

func downloadSourceCode(submit SubmitT) (string, error) {
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

func langConfig(submit *SubmitT) error {
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
	case "cs_dotnet31": // C#
		submit.compileCmd = "mkdir main && cd main && dotnet new console && mv ./../Main.cs Program.cs && dotnet publish -c Release --nologo -v q -o . 2>> ../userStderr.txt && cd /"
		submit.executeCmd = "dotnet ./main/main.dll < testcase.txt > userStdout.txt 2> userStderr.txt"
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
		return nil, err
	}

	DBMS := os.Getenv("DBMS")
	DBNAME := os.Getenv("DB_NAME")
	USER := os.Getenv("DB_USER")
	PASS := os.Getenv("DB_PASS")
	HOST := os.Getenv("DB_HOST")
	PORT := os.Getenv("DB_PORT")

	PROTOCOL := fmt.Sprintf("tcp(%s:%s)", HOST, PORT)

	CONNECT := USER + ":" + PASS + "@" + PROTOCOL + "/" + DBNAME + "?charset=utf8&parseTime=true&loc=Asia%2FTokyo"

	return gorm.Open(DBMS, CONNECT)
}

func main() {
	cmdChickets := CmdTicket{channel: make(map[string]chan CmdResultJSON)}

	go manageCmds(&cmdChickets)

	db, err := sqlConnect()
	if err != nil {
		log.Fatal(err)
	}

	for {
		var res []SubmitGORM
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
				cmdChickets.channel[strconv.FormatInt(res[i].ID, 10)] = make(chan CmdResultJSON)
				cmdChickets.Unlock()

				go judge(res[i], &cmdChickets)
			}
		}
	}
}
