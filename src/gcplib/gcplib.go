package gcplib

import (
	"bufio"
	"context"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
	"github.com/cafecoder-dev/cafecoder-judge/src/types"
	"google.golang.org/api/option"
)

const credentialFilePath = "./key.json"

var client *storage.Client
var once sync.Once

func DownloadSourceCode(ctx context.Context, submit types.SubmitT) (string, error) {
	once.Do(func() {
		c, _ := storage.NewClient(ctx, option.WithCredentialsFile(credentialFilePath))
		client = c
	})

	var fileName = strings.Split(submit.Info.Path, "/")[1]
	savePath := "./codes/" + fileName
	fp, err := os.Create(savePath)
	if err != nil {
		return "", err
	}

	bucket := "cafecoder-submit-source"
	obj := client.Bucket(bucket).Object(submit.Info.Path)
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

	return savePath, nil
}

func DownloadTestcase(ctx context.Context, problemUUID string, testcaseName string) ([]byte, string, error) {
	once.Do(func() {
		c, _ := storage.NewClient(ctx, option.WithCredentialsFile(credentialFilePath))
		client = c
	})

	bucketName := "cafecoder-testcase"
	bucket := client.Bucket(bucketName)

	reader, err := bucket.Object(problemUUID + "/input/" + testcaseName).NewReader(ctx)
	if err != nil {
		return nil, "", err
	}
	defer reader.Close()

	inputData, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, "", err
	}

	reader, err = bucket.Object(problemUUID + "/output/" + testcaseName).NewReader(ctx)
	if err != nil {
		return nil, "", err
	}
	defer reader.Close()

	outputData, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, "", err
	}

	return inputData, string(outputData), nil
}
