package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/offchainlabs/nitro/arbnode"
)

func TestEspressoConfigParsing(t *testing.T) {
	inputSource := map[string]interface{}{
		"sequencer": true,
		"espresso": map[string]interface{}{
			"caff-node": map[string]interface{}{
				"enable": true,
			},
			"batch-poster": map[string]interface{}{
				"tee-type":      "NITRO",
				"hotshot-url":   "http://localhost:8080",
				"tx-size-limit": int64(900000),
			},
			"streamer": map[string]interface{}{
				"hotshot-block": uint64(100),
				"dangerous": map[string]interface{}{
					"minimum-hotshot-block-num": int64(50),
				},
				"txns-polling-interval": "3s",
				"address-monitor-step":  uint64(100),
			},
		},
	}

	k := koanf.New(".")
	err := k.Load(confmap.Provider(inputSource, "."), nil)
	require.NoError(t, err)

	var parsedConfig arbnode.Config
	err = k.UnmarshalWithConf("", &parsedConfig, koanf.UnmarshalConf{Tag: "koanf"})
	require.NoError(t, err)

	assert.Equal(t, true, parsedConfig.Sequencer)
	assert.Equal(t, true, parsedConfig.Espresso.CaffNode.Enable)
	assert.Equal(t, "NITRO", parsedConfig.Espresso.BatchPoster.TeeType)
	assert.Equal(t, "http://localhost:8080", parsedConfig.Espresso.BatchPoster.HotShotUrl)
	assert.Equal(t, int64(900000), parsedConfig.Espresso.BatchPoster.TxSizeLimit)
	assert.Equal(t, uint64(100), parsedConfig.Espresso.Streamer.HotShotBlock)
	assert.Equal(t, uint64(50), parsedConfig.Espresso.Streamer.Dangerous.MinimumHotshotBlockNum)
	assert.Equal(t, 3*time.Second, parsedConfig.Espresso.Streamer.TxnsPollingInterval)
	assert.Equal(t, uint64(100), parsedConfig.Espresso.Streamer.AddressMonitorStep)
}

func TestEspressoConfigMigration(t *testing.T) {
	const oldConfigJSONPath = "./testdata/migrated_config.json"

	oldConfigJSON, err := os.ReadFile(oldConfigJSONPath)
	require.NoError(t, err)

	k := koanf.New(".")

	err = k.Load(
		rawbytes.Provider([]byte(oldConfigJSON)),
		json.Parser(),
	)
	require.NoError(t, err)

	var cfg arbnode.Config
	err = k.Unmarshal("", &cfg)
	require.NoError(t, err)

	// streamer
	require.Equal(t, uint64(10), cfg.Espresso.Streamer.HotShotBlock)
	require.Equal(
		t,
		uint64(56),
		cfg.Espresso.Streamer.Dangerous.MinimumHotshotBlockNum,
	)

	// caff-node
	require.True(
		t,
		cfg.Espresso.CaffNode.Dangerous.IgnoreDatabaseHotshotBlock,
	)
	require.False(
		t,
		cfg.Espresso.CaffNode.Dangerous.IgnoreDatabaseFromBlock,
	)

	require.Equal(t,
		time.Duration(2*time.Second),
		cfg.Espresso.Streamer.TxnsPollingInterval,
	)

	require.Equal(t,
		uint64(100),
		cfg.Espresso.Streamer.AddressMonitorStep,
	)
}
