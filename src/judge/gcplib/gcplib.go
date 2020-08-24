package gcplib

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/cafecoder-dev/cafecoder-judge/src/judge/types"
	"google.golang.org/api/option"
)

func DownloadSourceCode(ctx context.Context, submit types.SubmitT) (string, error) {
	credentialFilePath := "./key.json"

	client, err := storage.NewClient(ctx, option.WithCredentialsFile(credentialFilePath))
	if err != nil {
		return "", err
	}

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
