package arbnode

import (
	"context"
	"errors"
	lightclient "github.com/EspressoSystems/espresso-sequencer-go/light-client"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/util/stopwaiter"
	"sync"
	"time"
)

// A HotShotMonitor is a background process compatible with the `stopwaiter` that polls the light client contract to monitor if HotShot consensus is live.
// It can be used by any node component that bases its actions around hotshot's status to make these decisions in a background process.
// In theory a HotShotMonitor can be used by several node components at once, but only if constructed correctly with respect to the use of shared mutexes.
// In this case the mutex *must* be copied to every component that needs to access the shared boolean before it is used in any place.
// Failure to follow this patter will result in undefined behavior
type HotShotMonitor struct {
	stopwaiter.StopWaiterSafe
	// Reader needed to communicate with the espresso network and determine HotShot's status.
	lightClientReader lightclient.LightClientReaderInterface

	// Duration constants used for configuring how often we poll hotshot, and decide if it is live.
	pollInterval         time.Duration
	switchDelayThreshold uint64

	// Mutex shared by the batch poster and hotshot monitor to read/write to the boolean reference used for the escape hatch.
	escapeHatchMutex *sync.Mutex
	// Pointer to boolean that controls the escape hatch.
	escapeHatchOpen *bool
}

// NewHotShotMonitor creates a new instance of HotShotMonitor
// Params:
//
//	lightClientReader: An interface to communicate with the light client contract on the rollup destination.
//	pollInterval: A duration representing how often to poll HotShot
//	switchDelayThreshold: A uint64 parameter to a lightClient method that determines if hotshot is live
//	escapeHatchMutex: A shared mutex between the spawning process and this process used to protect the shared boolean reference.
//	escapeHatch: A pointer to a boolean shared between the spawning process and this process, used to tell the spawning process if HotShot is not live.
//
// Returns:
//
//	Success: A new HotShotMonitor ready to use.
//
// Errors:
//
//	In the event that any parameters are nil, this function will return an error as the HotShotMonitor would not be able to operate.
func NewHotShotMonitor(lightClientReader lightclient.LightClientReaderInterface, pollInterval time.Duration, switchDelayThreshold uint64, escapeHatchMutex *sync.Mutex, escapeHatchOpen *bool) (*HotShotMonitor, error) {
	if lightClientReader == nil {
		return nil, errors.New("NewHotShotMonitor: lightClientReader is nil")
	}
	if pollInterval == 0 {
		return nil, errors.New("NewHotShotMonitor: pollInterval is 0")
	}
	if switchDelayThreshold == 0 {
		return nil, errors.New("NewHotShotMonitor: switchDelayThreshold is 0")
	}
	if escapeHatchMutex == nil {
		return nil, errors.New("NewHotShotMonitor: escapeHatchMutex is nil")
	}
	if escapeHatchOpen == nil {
		return nil, errors.New("NewHotShotMonitor: escapeHatchOpen is nil")
	}
	monitor := &HotShotMonitor{
		lightClientReader:    lightClientReader,
		escapeHatchMutex:     escapeHatchMutex,
		escapeHatchOpen:      escapeHatchOpen,
		pollInterval:         pollInterval,
		switchDelayThreshold: switchDelayThreshold,
	}
	return monitor, nil
}

// monitorHotshotLiveness is the work function for this types task.
// It will periodically poll the light client on the rollup destination to determine if HotShot is live.
// As necessary, it will update the shared boolean reference to alert it's parent process.
func (m *HotShotMonitor) monitorHotshotLiveness(_ context.Context) time.Duration {
	isHotShotLive, err := m.lightClientReader.IsHotShotLive(m.switchDelayThreshold)
	switch isHotShotLive {
	case true:
		// IsHotShotLive can never return (true, err) it will always return either (false, err), (false, nil), or (true, nil)
		// Therefore if isHotShotLive is true, we need not check that err is nil
		m.escapeHatchMutex.Lock()
		// in the case that hotshot is live, we can close (or keep closed) the escape hatch
		if *m.escapeHatchOpen {
			log.Info("Hotshot has regained liveness. closing the Espresso escape hatch")
		}
		m.escapeHatchOpen = new(bool) // booleans are by default false in go, This is just an easy way to set the boolean to false.
		m.escapeHatchMutex.Unlock()
	case false:
		if err != nil {
			log.Warn("Failed to check if HotShot is live, opening escape hatch", "err", err)
		} else {
			log.Warn("HotShot is not live, opening escape hatch")
		}
		m.escapeHatchMutex.Lock()
		temp := true // We need to save this boolean in memory to be able to declare a pointer for it.
		m.escapeHatchOpen = &temp
		m.escapeHatchMutex.Unlock()
	}
	return m.pollInterval
}

// Start can be called on an instance of HotShotMonitor to have it begin monitoring HotShot.
// Errors:
//
//	Will print an error if it occurs in CallIterativelySafe. The main work function of this thread will not error.
//	Therefore, any errors will be external to the hotshot monitor after instantiation.
func (m *HotShotMonitor) Start(ctxIn context.Context) {
	err := m.StopWaiterSafe.Start(ctxIn, m)
	if err != nil {
		log.Error("Error starting StopWaiterSafe in HotShotMonitor.Start", "err", err)
	}
	err = m.CallIterativelySafe(m.monitorHotshotLiveness)
	if err != nil {
		log.Error("Error in CallIterativelySafe in HotShotMonitor.Start", "err", err)
	}
}
