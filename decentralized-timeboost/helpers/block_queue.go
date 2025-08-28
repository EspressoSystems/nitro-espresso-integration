package decentralized_timeboost

import (
	"sync"

	// Protobuf imports for grpc calls
	protos "github.com/EspressoSystems/timeboost-proto/go-generated"
)

type SynchronizedTimeboostBlockQueue struct {
	queue []*protos.Block
	mutex sync.RWMutex
}

func (q *SynchronizedTimeboostBlockQueue) Enqueue(item *protos.Block) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	q.queue = append(q.queue, item)
}

func (q *SynchronizedTimeboostBlockQueue) Dequeue() {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if len(q.queue) > 0 {
		q.queue = q.queue[1:]
	}
}

func (q *SynchronizedTimeboostBlockQueue) Peek() *protos.Block {
	q.mutex.RLock()
	defer q.mutex.RUnlock()
	if len(q.queue) == 0 {
		return nil
	}
	return q.queue[0]
}
