package gethexec

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"

	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type TimeboostListener struct {
	stopwaiter.StopWaiter
	config TimeboostListenerConfig
}

type TimeboostListenerConfig struct {
	ListenPort uint16 `koanf:"listen-port"`
}

var DefaultTimeboostListenerConfig = TimeboostListenerConfig{
	ListenPort: 55000,
}

func NewTimeboostListener() (*TimeboostListener, error) {
	return &TimeboostListener{
		config: TimeboostListenerConfig{
			ListenPort: 55000,
		},
	}, nil
}

func handleConnection(conn net.Conn, txChan chan<- []byte) {
	defer conn.Close()
	for {
		sizeBuf := make([]byte, 4)
		_, err := conn.Read(sizeBuf)
		if err != nil {
			log.Error("Txn listener error reading data size", "err", err)
			return
		}

		size := binary.BigEndian.Uint32(sizeBuf)

		data := make([]byte, size)
		_, err = conn.Read(data)
		if err != nil {
			log.Error("Txn listener error reading data", "err", err)
			return
		}
		txChan <- data

		_, err = conn.Write([]byte{0xc0})
		if err != nil {
			log.Error("Txn listener srror sending acknowledge to timeboost", "err", err)
			return
		}

	}
}

func listenAndServe(port uint16, txChan chan<- []byte) {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("Txn listener failed to start", "port", port, "err", err)
		return
	}
	defer listener.Close()
	log.Info("Listening", "port", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Info("Connection accept error", "port", port, "err", err)
			continue
		}
		go handleConnection(conn, txChan)
	}
}

func (s *TimeboostListener) Start(ctx context.Context, txChan chan<- []byte) {
	s.StopWaiter.Start(ctx, s)
	s.LaunchThread(func(ctx context.Context) {
		listenAndServe(s.config.ListenPort, txChan)
	})

}

func (s *TimeboostListener) StopAndWait() {
	s.StopWaiter.StopAndWait()
}
