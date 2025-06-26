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
	conn   net.Conn
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
		conn: nil,
	}, nil
}

func (l *TimeboostListener) HasConnection() bool {
	return l.conn != nil
}

func (l *TimeboostListener) Receive() ([]byte, error) {
	sizeBuf := make([]byte, 4)
	_, err := l.conn.Read(sizeBuf)
	if err != nil {
		log.Error("Txn listener error reading data size", "err", err)
		return nil, err
	}

	size := binary.BigEndian.Uint32(sizeBuf)

	inclBytes := make([]byte, size)
	_, err = l.conn.Read(inclBytes)
	if err != nil {
		log.Error("Txn listener error reading data", "err", err)
		return nil, err
	}
	return inclBytes, nil
}

func (l *TimeboostListener) WriteAck() error {
	_, err := l.conn.Write([]byte{0xc0})
	if err != nil {
		log.Error("Txn listener srror sending acknowledge to timeboost", "err", err)
		return err
	}
	return nil
}

func (l *TimeboostListener) listenAndServe(port uint16) error {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("Txn listener failed to start", "port", port, "err", err)
		return err
	}
	defer listener.Close()
	log.Info("Listening", "port", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Info("Connection accept error", "port", port, "err", err)
			continue
		}
		l.conn = conn
	}
}

func (l *TimeboostListener) Start(ctx context.Context) {
	l.StopWaiter.Start(ctx, l)
	l.LaunchThread(func(ctx context.Context) {
		err := l.listenAndServe(l.config.ListenPort)
		if err != nil {
			panic("Failed to start listener")
		}
	})
}

func (l *TimeboostListener) StopAndWait() {
	l.StopWaiter.StopAndWait()
}
