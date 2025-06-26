package gethexec

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	flag "github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/log"

	"github.com/offchainlabs/nitro/util/stopwaiter"
)

// Acknowledgement flag that timeboost will wait for
// This is to know sequencer processed Inclusion list succesfully
const ACK_FLAG = 0xc0

type TimeboostListener struct {
	stopwaiter.StopWaiter
	config         TimeboostListenerConfig
	conn           net.Conn
	connectionLock sync.Mutex
}

type TimeboostListenerConfig struct {
	ListenPort    uint16        `koanf:"listen-port"`
	ReadDeadline  time.Duration `koanf:"read-dealine"`
	WriteDeadline time.Duration `koanf:"write-dealine"`
	MaxBackoff    time.Duration `koanf:"max-backoff"`
}

var DefaultTimeboostListenerConfig = TimeboostListenerConfig{
	ListenPort:    55000,
	ReadDeadline:  5 * time.Second,
	WriteDeadline: 5 * time.Second,
	MaxBackoff:    5 * time.Second,
}

func TimeboostListenerConfigAddOptions(prefix string, f *flag.FlagSet) {
	f.Uint16(prefix+".listen-port", DefaultTimeboostListenerConfig.ListenPort, "timeboost transaction listener listen port")
	f.Duration(prefix+".read-dealine", DefaultTimeboostListenerConfig.ReadDeadline, "timeboost transaction listener read deadline")
	f.Duration(prefix+".write-dealine", DefaultTimeboostListenerConfig.WriteDeadline, "timeboost transaction listener write deadline")
	f.Duration(prefix+".max-backoff", DefaultTimeboostListenerConfig.MaxBackoff, "timeboost transaction listener max backoff")
}

// Result from the listener accepting new connections
type connectionResult struct {
	conn net.Conn
	err  error
}

func NewTimeboostListener(config TimeboostListenerConfig) (*TimeboostListener, error) {
	return &TimeboostListener{
		config: config,
		conn:   nil,
	}, nil
}

/**
 * This function checks to see if we currently have a connection
 */
func (l *TimeboostListener) HasConnection() bool {
	l.connectionLock.Lock()
	defer l.connectionLock.Unlock()
	return l.conn != nil
}

/**
 * This function receives the encoded inclusion list from timeboost and has a deadline for each read operation
 * 1.) Read the encoded inclusion list bytes (u32) size
 * 2.) Read the exact bytes of encoded inclusion list
 */
func (l *TimeboostListener) ReceiveInclusionList() ([]byte, error) {
	l.connectionLock.Lock()
	defer l.connectionLock.Unlock()
	// Read encoded inclusion list size
	if err := l.setReadDeadline(); err != nil {
		return nil, l.onError("Timeboost txn listener error setting read deadline for reading size", err)
	}

	sizeBuf := make([]byte, 4)
	if _, err := l.conn.Read(sizeBuf); err != nil {
		return nil, l.onError("Timeboost txn listener error reading data size", err)
	}

	// Read inclusion list
	if err := l.setReadDeadline(); err != nil {
		return nil, l.onError("Timeboost txn listener error setting read deadline for reading data", err)
	}

	inclBytes := make([]byte, binary.BigEndian.Uint32(sizeBuf))
	if _, err := l.conn.Read(inclBytes); err != nil {
		return nil, l.onError("Timeboost txn listener error reading data", err)
	}
	return inclBytes, nil
}

/**
 * This function sends an acknowledgement flag back to timeboost AFTER it successfully processes the transactions
 */
func (l *TimeboostListener) WriteAck() error {
	l.connectionLock.Lock()
	defer l.connectionLock.Unlock()
	// Send back acknowledgement to timeboost so it knows it can move on
	if err := l.setWriteDeadline(); err != nil {
		return l.onError("Timeboost txn listener error setting write deadline for reading data", err)
	}
	if _, err := l.conn.Write([]byte{ACK_FLAG}); err != nil {
		return l.onError("Timeboost txn listener error writing acknowledgement", err)
	}
	return nil
}

/**
 * This function sets the read deadline with the config value
 */
func (l *TimeboostListener) setReadDeadline() error {
	deadline := time.Now().Add(l.config.ReadDeadline)
	err := l.conn.SetReadDeadline(deadline)
	if err != nil {
		return err
	}

	return nil
}

/**
 * This function sets the write deadline with the config value
 */
func (l *TimeboostListener) setWriteDeadline() error {
	deadline := time.Now().Add(l.config.WriteDeadline)
	err := l.conn.SetWriteDeadline(deadline)
	if err != nil {
		return err
	}

	return nil
}

/**
 * This function logs and error that happening over the connection and closes it.
 * Timeboost will retry to connect
 */
func (l *TimeboostListener) onError(msg string, err error) error {
	// If any error close the connection
	// Timeboost will keep retrying to establish a new tcp connection
	log.Error(msg, "err", err)
	l.shutdown()
	return err
}

/**
 * This function closes the connection and reassigns it to nil
 */
func (l *TimeboostListener) shutdown() {
	if l.conn != nil {
		err := l.conn.Close()
		if err != nil {
			log.Error("Timeboost txn listener error closing connection", err)
		}
		l.conn = nil
	}
}

/**
 * This function listens for incoming connections in its own go routine
 * If there is another successful connection we drop the old connection
 */
func (l *TimeboostListener) connectionHandler(ctx context.Context, port uint16) error {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("Timeboost txn listener failed to start", "port", port, "err", err)
		return err
	}
	defer listener.Close()
	log.Info("Timeboost txn listener is listening", "port", port)

	connCh := make(chan connectionResult)
	go func() {
		// Incase of failures, timeboost will continuously disconnect and reconnect
		// So keep listening
		for {
			conn, err := listener.Accept()
			if err != nil {
				connCh <- connectionResult{nil, err}
			} else {
				connCh <- connectionResult{conn, nil}
			}
		}
	}()

	for {
		select {
		case conn := <-connCh:
			if conn.err != nil {
				log.Error("Timeboost txn listener connection accept error", "port", port, "err", err)
				continue
			}
			log.Info("Received connection", "addr", conn.conn.RemoteAddr())
			// There will only ever be 1 connection at a time between timeboost and sequencer
			// So make sure old connection is closed, and assign it the new connection
			l.connectionLock.Lock()
			l.shutdown()
			l.conn = conn.conn
			l.connectionLock.Unlock()
		case <-ctx.Done():
			l.shutdown()
			log.Info("Timeboost txn listener has been terminated")
			return nil
		}
	}
}

func (l *TimeboostListener) Start(ctx context.Context) {
	l.StopWaiter.Start(ctx, l)
	l.LaunchThread(func(ctx context.Context) {
		err := l.connectionHandler(ctx, l.config.ListenPort)
		if err != nil {
			panic("Failed to start listener")
		}
	})
}

func (l *TimeboostListener) StopAndWait() {
	l.StopWaiter.StopAndWait()
}
