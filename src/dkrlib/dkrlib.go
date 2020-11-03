package dkrlib

// dkrlib ... docker 系のライブラリ。名前がやばい

import (
	"archive/tar"
	"bytes"
	"context"
	"io/ioutil"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

const apiVersion = "1.40"

type Container struct {
	Client    *client.Client
	Name      string
	ID        string
	IPAddress string
}

// CreateContainer ... create new container and return container information
func CreateContainer(ctx context.Context, containerName string) (*Container, error) {
	var err error
	pidsLimit := int64(1024)

	cli, err := client.NewClientWithOpts(client.WithVersion(apiVersion))
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	config := &container.Config{Image: "cafecoder"}
	hostConfig := &container.HostConfig{
		Resources: container.Resources{
			Memory:    2048000000, // メモリの制限: 2048 MB
			PidsLimit: &pidsLimit,
		},
	}

	resp, err := cli.ContainerCreate(ctx, config, hostConfig, nil, nil, containerName)
	if err != nil {
		return nil, err
	}

	err = cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{})
	if err != nil {
		return nil, err
	}

	containerInspect, err := cli.ContainerInspect(ctx, resp.ID)
	if err != nil {
		return nil, err
	}

	return &Container{
		Client:    cli,
		Name:      containerName,
		ID:        resp.ID,
		IPAddress: containerInspect.NetworkSettings.IPAddress,
	}, nil
}

// RemoveContainer ... コンテナを破棄する
func (container *Container) RemoveContainer(ctx context.Context) {
	_ = container.Client.ContainerStop(ctx, container.ID, nil)
	_ = container.Client.ContainerRemove(
		ctx,
		container.ID,
		types.ContainerRemoveOptions{RemoveVolumes: true, RemoveLinks: true, Force: true},
	)

	labelFilters := filters.NewArgs()
	_, _ = container.Client.ContainersPrune(ctx, labelFilters)
}

// CopyFromContainer ... コンテナからコピーしてくる
func (container *Container) CopyFromContainer(ctx context.Context, filepath string) (*bytes.Buffer, error) {
	var buffer *bytes.Buffer
	reader, _, err := container.Client.CopyFromContainer(
		ctx,
		container.ID,
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
func (container *Container) CopyToContainer(ctx context.Context, hostFilePath string, containerFilePath string, mode int64) error {
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
	defer tw.Close()

	err = tw.WriteHeader(
		&tar.Header{
			Name: containerFilePath,
			Mode: mode,
			Size: int64(len(content)),
		},
	)
	if err != nil {
		return err
	}

	if _, err := tw.Write(content); err != nil {
		return err
	}

	err = container.Client.CopyToContainer(
		ctx,
		container.ID,
		"/",
		&buf,
		types.CopyToContainerOptions{},
	)
	if err != nil {
		return err
	}

	return nil
}
