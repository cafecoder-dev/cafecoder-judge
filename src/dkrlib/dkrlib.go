package dkrlib

// dkrlib ... docker 系のライブラリ。名前がやばい

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/cafecoder-dev/cafecoder-judge/src/types"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	docker_types "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

const apiVersion = "1.40"

// CreateContainer ... コンテナを作成する
func CreateContainer(ctx context.Context, submit *types.SubmitT) error {
	var err error

	submit.ContainerCli, err = client.NewClientWithOpts(client.WithVersion(apiVersion))

	if err != nil {
		return err
	}
	defer submit.ContainerCli.Close()

	config := &container.Config{Image: "cafecoder"}
	hostConfig := &container.HostConfig{
		Resources: container.Resources {
			Memory: 2048000000, // 2048 MB
		},
	}
	

	resp, err := submit.ContainerCli.ContainerCreate(ctx, config, hostConfig, nil, nil, submit.HashedID)
	if err != nil {
		return err
	}

	submit.ContainerID = resp.ID
	err = submit.ContainerCli.ContainerStart(ctx, submit.ContainerID, docker_types.ContainerStartOptions{})
	if err != nil {
		return err
	}


	submit.ContainerInspect, err = submit.ContainerCli.ContainerInspect(ctx, submit.ContainerID)
	if err != nil {
		return err
	}
	return nil
}

// RemoveContainer ... コンテナを破棄する
func RemoveContainer(ctx context.Context, submit types.SubmitT) {
	submit.ContainerCli.ContainerStop(ctx, submit.ContainerID, nil)
	submit.ContainerCli.ContainerRemove(
		ctx, 
		submit.ContainerID, 
		docker_types.ContainerRemoveOptions{RemoveVolumes: true, RemoveLinks: true, Force: true},
	)

	labelFilters := filters.NewArgs()

	_, _ = submit.ContainerCli.ContainersPrune(ctx, labelFilters)

	fmt.Println("container: " + submit.ContainerID + " removed")
}

// CopyFromContainer ... コンテナからコピーしてくる
func CopyFromContainer(ctx context.Context, filepath string, submit types.SubmitT) (*bytes.Buffer, error) {
	var buffer *bytes.Buffer
	reader, _, err := submit.ContainerCli.CopyFromContainer(
		ctx,
		submit.ContainerID,
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

// CopyToContainer ... コンテナにコピーする
func CopyToContainer(ctx context.Context, hostFilePath string, containerFilePath string, mode int64, submit types.SubmitT) error {
	var buf bytes.Buffer

	usercodeFile, err := os.Open(hostFilePath)
	if err != nil {
		return err
	}
	defer usercodeFile.Close()

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

	err = submit.ContainerCli.CopyToContainer(
		ctx,
		submit.ContainerID,
		"/",
		&buf,
		docker_types.CopyToContainerOptions{},
	)
	if err != nil {
		return err
	}

	return nil
}
