package utils

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/EspressoSystems/espresso-network/sdks/go/types"
	"github.com/ethereum/go-ethereum/log"
)

const (
	NUM_NSS_BYTE_LEN = 4 // num entries field
	NS_ID_BYTE_LEN   = 4 // ns_id is u32
	OFFSET_BYTE_LEN  = 4 // offset is u32
	ENTRY_BYTE_LEN   = NS_ID_BYTE_LEN + OFFSET_BYTE_LEN
	LEN_SIZE         = 4
	NUM_TXS_BYTE_LEN = 4
)

type NSRange struct {
	NsId  uint32
	Start uint32
	End   uint32
}

func DecodeTransactionsPayload(encoded []byte, nsRange NSRange) ([]types.Transaction, error) {

	if len(encoded) < int(nsRange.End) {
		return nil, fmt.Errorf("encoded payload is smaller than the end of the ns range")
	}
	nsEncodedPayload := encoded[nsRange.Start:nsRange.End]

	r := bytes.NewReader(nsEncodedPayload)

	// Step 1: num_txs
	var numTxs uint32
	err := binary.Read(r, binary.LittleEndian, &numTxs)
	if err != nil {
		return nil, fmt.Errorf("read num txs: %w", err)
	}

	// Step 2: offsets
	offsets := make([]uint32, numTxs)
	for i := range offsets {
		if err := binary.Read(r, binary.LittleEndian, &offsets[i]); err != nil {
			return nil, fmt.Errorf("read offsets: %w", err)
		}
	}

	log.Info("num txs of decode transactions", "numTxs", numTxs)
	log.Info("offsets of decode transactions", "offsets", offsets)

	// Step 3: tx_bodies = remaining bytes
	txBodies, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read tx_bodies: %w", err)
	}

	// Step 4: reconstruct transactions
	var txs []types.Transaction
	start := uint32(0)
	for i := 0; i < int(numTxs); i++ {
		end := start + offsets[i]
		txBytes := txBodies[start:end]
		log.Info("start of decode transactions", "start", start)
		log.Info("end of decode transactions", "end", end)
		log.Info("txBytes of decode transactions", "txBytes", txBytes)
		txs = append(txs, types.Transaction{
			Payload: txBytes,
		})
		start = end
	}

	log.Info("transaction after decoding", "txs", txs)
	return txs, nil
}

func DecodeNSTable(encoded []byte, namespaceId uint32) (*NSRange, error) {
	if len(encoded) < NUM_NSS_BYTE_LEN {
		return nil, fmt.Errorf("ns table too short: %d bytes", len(encoded))
	}

	numEntries := binary.LittleEndian.Uint32(encoded[:NUM_NSS_BYTE_LEN])

	off := NUM_NSS_BYTE_LEN

	start := uint32(0)

	for i := uint32(0); i < numEntries; i++ {
		nsId := binary.LittleEndian.Uint32(encoded[off : off+NS_ID_BYTE_LEN])
		off += NS_ID_BYTE_LEN
		offset := binary.LittleEndian.Uint32(encoded[off : off+OFFSET_BYTE_LEN])
		off += OFFSET_BYTE_LEN

		end := start + offset

		if namespaceId == nsId {
			log.Info("nsId of ns table", "nsId", nsId)
			log.Info("offset of ns table", "offset", offset)
			log.Info("end of ns table", "end", end)
			log.Info("start of ns table", "start", start)
			return &NSRange{
				NsId:  namespaceId,
				End:   end,
				Start: start,
			}, nil
		}
		start = end
	}

	return nil, fmt.Errorf("ns table not found")
}

// 0...250
// 250..350
//
