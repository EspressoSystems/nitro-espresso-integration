package gethexec

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"

	"github.com/offchainlabs/nitro/util/stopwaiter"
)

type TimeboostListener struct {
	stopwaiter.StopWaiter
	config TimeboostListenerConfig
}

type TimeboostListenerConfig struct {
	Enable     bool   `koanf:"enable"`
	ListenPort uint16 `koanf:"listen-port"`
}

var DefaultTimeboostListenerConfig = TimeboostListenerConfig{
	Enable:     true,
	ListenPort: 55000,
}

func NewTimeboostListener() (*TimeboostListener, error) {
	return &TimeboostListener{
		config: TimeboostListenerConfig{
			Enable:     true,
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
			log.Printf("Error reading size: %v", err)
			return
		}

		size := binary.BigEndian.Uint32(sizeBuf)
		log.Printf("Received %d", size)

		data := make([]byte, size)
		_, err = conn.Read(data)
		if err != nil {
			log.Printf("Error reading data: %v", err)
			return
		}
		txChan <- data

		_, err = conn.Write([]byte{0xc0})
		if err != nil {
			return
		}

	}
}

func listenAndServe(port uint16, txChan chan<- []byte) {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Failed to listen on %d: %v", port, err)
		return
	}
	defer listener.Close()
	log.Printf("Listening on :%d", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error on port %d: %v", port, err)
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
