package arbnode

import (
	"context"
	lightclient "github.com/EspressoSystems/espresso-sequencer-go/light-client"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/util/stopwaiter"
	"sync"
	"time"
)

type HotshotMonitor struct {
	stopwaiter.StopWaiterSafe

	pollInterval         time.Duration
	switchDelayThreshold uint64

	lightClientReader lightclient.LightClientReaderInterface
	escapeHatchMutex  *sync.Mutex
	//pointer to boolean that controls the escape hatch.
	escapeHatchOpen *bool
}

func NewHotshotMonitor(lightClientReader lightclient.LightClientReaderInterface, escapeHatchMutex *sync.Mutex, escapeHatch *bool) (*HotshotMonitor, error) {
	monitor := &HotshotMonitor{
		lightClientReader: lightClientReader,
		escapeHatchMutex:  escapeHatchMutex,
		escapeHatchOpen:   escapeHatch,
	}
	return monitor, nil
}

func (m *HotshotMonitor) monitorHotshotLiveness(_ context.Context) time.Duration {
	isHotShotLive, err := m.lightClientReader.IsHotShotLive(m.switchDelayThreshold)
	switch isHotShotLive {
	case true:
		// IsHotShotLive can never return (true, err) it will always return either (false, err), (false, nil), or (true, nil)
		//Therefore if isHotShotLive is true, we need not check that err is nil
		m.escapeHatchMutex.Lock()
		//in the case that hotshot is live, we can close (or keep closed) the escape hatch
		if *m.escapeHatchOpen {
			log.Info("Hotshot has regained liveness. closing the Espresso escape hatch")
		}
		m.escapeHatchOpen = new(bool) // booleans are by default false in go, This is just an easy way to set the boolean to false.
		m.escapeHatchMutex.Unlock()
	case false:
		if err != nil {
			log.Warn("Failed to check if HotShot is live, opening escape hatch", "err", err)
		}
		m.escapeHatchMutex.Lock()
		temp := true // We need to save this boolean in memory to be able to declare a pointer for it.
		m.escapeHatchOpen = &temp
		m.escapeHatchMutex.Unlock()

	}
	return m.pollInterval
}

func (m *HotshotMonitor) Start(ctxIn context.Context) error {
	//TODO: do we need to add a stop waiter and take a ctx as a param in this function?
	m.StopWaiterSafe.Start(ctxIn, m)

	if m.lightClientReader != nil && m.escapeHatchOpen != nil && m.escapeHatchMutex != nil {
		err := m.CallIterativelySafe(m.monitorHotshotLiveness)
		if err != nil {
			log.Error("Error in HotshotMonitor.Start", "err", err)
			return err
		}
	}
	return nil
}
