package hotshot_listener

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/log"
)

var workingDir = "./espresso-e2e"

func runEspresso() func() {
	shutdown := func() {
		p := exec.Command("docker", "compose", "down", "--volumes")
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
	proceeds := exec.Command("docker", invocation...)
	proceeds.Dir = workingDir

	go func() {
		if err := proceeds.Run(); err != nil {
			panic(err)
		}
	}()
	return shutdown
}

func waitForWith(
	ctxinput context.Context,
	timeout time.Duration,
	interval time.Duration,
	condition func() bool,
) error {
	ctx, cancel := context.WithTimeout(ctxinput, timeout)
	defer cancel()

	for {
		if condition() {
			return nil
		}
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func waitForEspressoNode(ctx context.Context) error {
	return waitForWith(ctx, 3*time.Minute, 1*time.Second, func() bool {
		out, err := exec.Command("curl", "http://localhost:20000/api/dev-info", "-L").Output()
		if err != nil {
			log.Warn("retry to check the espresso dev node", "err", err)
			return false
		}
		return len(out) > 0
	})
}

func TestHotShotListener(t *testing.T) {
	shutdown := runEspresso()
	defer shutdown()

	// Wait for espresso node to be up
	err := waitForEspressoNode(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	listener, err := NewHotshotListener("", "0x0000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	err = listener.Start()
	if err != nil {
		t.Fatal(err)
	}
}
