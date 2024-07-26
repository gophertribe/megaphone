package sip

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/suite"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

type EndpointSuite struct {
	suite.Suite
	client *client.Client
	cid    string
	wg     sync.WaitGroup
}

func TestEndpointSuite(t *testing.T) {
	suite.Run(t, &EndpointSuite{})
}

func (suite *EndpointSuite) TestRegister() {
	creds := Credentials{
		Username:        "megaphone",
		Password:        "1234",
		ContactHostname: "192.168.88.248",
	}
	dest := Destination{
		Transport: "udp",
		ProxyAddr: "192.168.88.248:5060",
	}
	endpoint, err := NewEndpoint(creds.Username, nil)
	suite.Require().NoError(err)
	ctx := context.Background()
	err = endpoint.Register(ctx, creds, dest)
	suite.Require().NoError(err)
}

func (suite *EndpointSuite) SetupSuite() {
	ctx := context.Background()
	err := suite.runAsteriskContainer(ctx, "andrius/asterisk:alpine-20.5.2", &suite.wg)
	suite.Require().NoError(err)
}

func (suite *EndpointSuite) TearDownSuite() {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	err := suite.client.ContainerStop(ctx, suite.cid, container.StopOptions{})
	r := suite.Require()
	r.NoError(err)
	err = suite.client.ContainerRemove(ctx, suite.cid, types.ContainerRemoveOptions{Force: true})
	r.NoError(err)
	suite.wg.Wait()
}

func (suite *EndpointSuite) runAsteriskContainer(ctx context.Context, image string, wg *sync.WaitGroup) error {
	var err error
	suite.client, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("could not initialize docker client: %w", err)
	}

	reader, err := suite.client.ImagePull(ctx, image, types.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("could not pull build image: %w", err)
	}
	_, _ = io.Copy(os.Stdout, reader)

	pwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine current path: %w", err)
	}
	binds := []string{
		fmt.Sprintf("%s/test/asterisk/ari.conf:/etc/asterisk/ari.conf", pwd),
		fmt.Sprintf("%s/test/asterisk/asterisk.conf:/etc/asterisk/asterisk.conf", pwd),
		fmt.Sprintf("%s/test/asterisk/extensions.conf:/etc/asterisk/extensions.conf", pwd),
		fmt.Sprintf("%s/test/asterisk/http.conf:/etc/asterisk/http.conf", pwd),
		fmt.Sprintf("%s/test/asterisk/pjsip.conf:/etc/asterisk/pjsip.conf", pwd),
		fmt.Sprintf("%s/test/asterisk/rtp.conf:/etc/asterisk/rtp.conf", pwd),
	}

	ports := nat.PortMap{}

	for _, def := range []string{
		"5060:5060/udp",
		"10000-10050:10000-10050",
	} {
		mappings, err := nat.ParsePortSpec(def)
		if err != nil {
			return fmt.Errorf("could not parse port spec from %s: %w", def, err)
		}
		for _, pm := range mappings {
			ports[pm.Port] = []nat.PortBinding{pm.Binding}
		}
	}

	resp, err := suite.client.ContainerCreate(ctx, &container.Config{Image: image}, &container.HostConfig{
		Binds:        binds,
		PortBindings: ports,
		AutoRemove:   false,
	}, nil, nil, "")
	if err != nil {
		return fmt.Errorf("could not create build container: %w", err)
	}

	suite.cid = resp.ID
	err = suite.client.ContainerStart(ctx, suite.cid, types.ContainerStartOptions{})
	if err != nil {
		return fmt.Errorf("could not start build container: %w", err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		statusCh, errCh := suite.client.ContainerWait(ctx, suite.cid, container.WaitConditionNotRunning)
		select {
		case err = <-errCh:
			err = fmt.Errorf("container wait error: %w", err)
		case status := <-statusCh:
			if status.Error != nil {
				err = fmt.Errorf("container exit error: %s", status.Error.Message)
			} else if status.StatusCode != 0 {
				err = fmt.Errorf("container exit code: %d", status.StatusCode)
			}
		}
		if err != nil {
			suite.Fail(err.Error())
		}

		out, errlog := suite.client.ContainerLogs(ctx, suite.cid, types.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
		})
		if errlog != nil {
			suite.Fail(fmt.Sprintf("could not init container log reader: %v\n", errlog))
		}
		_, _ = stdcopy.StdCopy(os.Stdout, os.Stderr, out)
	}()
	return nil
}
