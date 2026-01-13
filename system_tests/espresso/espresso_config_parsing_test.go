package espresso

import (
	"testing"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/providers/confmap"
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
				"espresso-tee-type":      "NITRO",
				"hotshot-url":            "http://localhost:8080",
				"espresso-tx-size-limit": int64(900000),
			},
			"streamer": map[string]interface{}{
				"hotshot-block": uint64(100),
				"dangerous": map[string]interface{}{
					"minimum-hotshot-block-num": int64(50),
				},
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
	assert.Equal(t, "NITRO", parsedConfig.Espresso.BatchPoster.EspressoTeeType)
	assert.Equal(t, "http://localhost:8080", parsedConfig.Espresso.BatchPoster.HotShotUrl)
	assert.Equal(t, int64(900000), parsedConfig.Espresso.BatchPoster.EspressoTxSizeLimit)
	assert.Equal(t, uint64(100), parsedConfig.Espresso.StreamerConfig.HotShotBlock)
	assert.Equal(t, uint64(50), parsedConfig.Espresso.StreamerConfig.Dangerous.MinimumHotshotBlockNum)
}
