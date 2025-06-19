package clientdevnode

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/stretchr/testify/assert"
)

var workingDir = "../client/dev-node"

func TestFetchDevInfo(t *testing.T) {
	ctx := context.Background()
	cleanup := runEspresso()
	defer cleanup()

	client := NewClient("http://localhost:20000/v0")

	for {
		available, err := client.IsAvailable(ctx)
		if available {
			break
		}
		fmt.Println("waiting for node to be available", err)
		time.Sleep(1 * time.Second)
	}

	devInfo, err := client.FetchDevInfo(ctx)
	if err != nil {
		t.Fatal("failed to fetch dev info", err)
	}
	assert.Equal(t, "http://localhost:23000/", devInfo.BuilderUrl)
	assert.Equal(t, 21000, int(devInfo.SequencerApiPort))
	// This serves as a reminder that the L1 light client address has changed when it breaks.
	assert.Equal(t, "0x9fe46736679d2d9a65f0992f2272de9f3c7fa6e0", devInfo.L1LightClientAddress)
}

func runEspresso() func() {
	shutdown := func() {
		p := exec.Command("docker", "compose", "down")
		p.Dir = workingDir
		err := p.Run()
		if err != nil {
			panic(err)
		}
	}

	shutdown()
	invocation := []string{"compose", "up", "-d", "--build"}
	nodes := []string{
		"espresso-dev-node",
	}
	invocation = append(invocation, nodes...)
	procees := exec.Command("docker", invocation...)
	procees.Dir = workingDir

	go func() {
		if err := procees.Run(); err != nil {
			log.Error(err.Error())
			panic(err)
		}
	}()
	return shutdown
}
