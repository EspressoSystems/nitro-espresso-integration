package espresso

import (
	"strings"
	"testing"

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
	assert.Equal(t, uint64(100), parsedConfig.Espresso.Streamer.HotShotBlock)
	assert.Equal(t, uint64(50), parsedConfig.Espresso.Streamer.Dangerous.MinimumHotshotBlockNum)
}

func TestEspressoConfigMigration(t *testing.T) {
	const migratedJSON = `{
			"chain": {
				"info-files": [
				"/config/l2_chain_info.json"
				]
			},
			"espresso": {
				"batch-poster": {
				"address-monitor-start-l1": 1,
				"address-valid-ranges": [
					{
					"address": "${address1}",
					"from": 1,
					"to": 99
					},
					{
					"address": "${address2}",
					"from": 99,
					"to": 199
					}
				],
				"espresso-register-service-config": {
					"max-base-fee": 10,
					"max-retries": 5
				},
				"espresso-tee-type": "${espresso_tee_type}",
				"hotshot-first-posting-block": 1,
				"hotshot-url": "https://localhost:9090",
				"resubmit-espresso-tx-deadline": "2m"
				},
				"caff-node": {
				"dangerous": {
					"ignore-database-from-block": false,
					"ignore-database-hotshot-block": true
				},
				"enable": false,
				"espresso-tee-type": "${espresso_tee_type}",
				"namespace": 23
				},
				"streamer": {
				"dangerous": {
					"minimum-hotshot-block-num": 56
				},
				"hotshot-block": 10
				}
			},
			"execution": {
				"forwarding-target": "null",
				"sequencer": {
				"enable": true
				}
			},
			"http": {
				"addr": "0.0.0.0",
				"api": [
				"eth",
				"net",
				"web3",
				"arb",
				"debug",
				"txpool"
				],
				"corsdomain": "*",
				"vhosts": "*"
			},
			"node": {
				"batch-poster": {
				"data-poster": {
					"max-base-fee": 10
				},
				"enable": true,
				"l1-block-bound": "ignore",
				"light-client-address": "${light_client_address}",
				"max-delay": "1h0m0s",
				"max-empty-batch-delay": "1h0m0s",
				"parent-chain-wallet": {
					"private-key": "${batch_poster_secret_arn}"
				},
				"poll-interval": "10s",
				"post-4844-blobs": false,
				"redis-url": "",
				"wait-for-max-delay": true
				},
				"block-validator": {
				"enable": true,
				"validation-server": {
					"jwtsecret": "/config/val_jwt.hex",
					"url": "${validation_server_url}"
				}
				},
				"dangerous": {
				"disable-blob-reader": true,
				"no-sequencer-coordinator": true
				},
				"data-availability": {
				"enable": false
				},
				"delayed-sequencer": {
				"enable": true,
				"finalize-distance": 6,
				"use-merge-finality": false
				},
				"feed": {
				"input": {
					"url": ""
				},
				"output": {
					"enable": true,
					"signed": false
				}
				},
				"parent-chain-reader": {
				"poll-interval": "60s",
				"poll-only": true
				},
				"seq-coordinator": {
				"enable": false,
				"lockout-duration": "30s",
				"lockout-spare": "1s",
				"my-url": "",
				"redis-url": "redis://redis:6379",
				"retry-interval": "0.5s",
				"seq-num-duration": "24h0m0s",
				"update-interval": "3s"
				},
				"sequencer": true,
				"staker": {
				"disable-challenge": false,
				"enable": true,
				"make-assertion-interval": "120s",
				"parent-chain-wallet": {
					"private-key": "${staker_secret_arn}"
				},
				"staker-interval": "120s",
				"strategy": "MakeNodes"
				}
			},
			"parent-chain": {
				"connection": {
				"url": "${parent_chain_url_secret_arn}"
				}
			},
			"persistent": {
				"chain": "local",
				"db-engine": "leveldb"
			},
			"ws": {
				"addr": "0.0.0.0",
				"api": [
				"eth",
				"net",
				"web3",
				"arb",
				"debug",
				"txpool"
				]
			}
			}
		`

	k := koanf.New(".")

	err := k.Load(
		rawbytes.Provider([]byte(strings.TrimSpace(migratedJSON))),
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
}
