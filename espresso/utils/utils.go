package utils

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/EspressoSystems/espresso-network/sdks/go/types"
)

const (
	NUM_NSS_BYTE_LEN = 4 // num entries field
	NS_ID_BYTE_LEN   = 4 // ns_id is u32
	OFFSET_BYTE_LEN  = 4 // offset is u32
	ENTRY_BYTE_LEN   = NS_ID_BYTE_LEN + OFFSET_BYTE_LEN
	LEN_SIZE         = 4
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
	numTxns := make([]uint8, 4)
	if _, err := io.ReadFull(r, numTxns); err != nil {
		return nil, fmt.Errorf("read num_txs: %w", err)
	}
	numTxs := binary.LittleEndian.Uint32(numTxns)

	// Step 2: offsets
	offsets := make([]uint32, numTxs)
	for i := range offsets {
		if err := binary.Read(r, binary.LittleEndian, &offsets[i]); err != nil {
			return nil, fmt.Errorf("read offset[%d]: %w", i, err)
		}
	}

	// Step 3: tx_bodies = remaining bytes
	txBodies, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read tx_bodies: %w", err)
	}

	// Step 4: reconstruct transactions
	var txs []types.Transaction
	start := uint32(0)
	for i := 0; i < int(numTxs); i++ {
		end := offsets[i]
		txBytes := txBodies[start:end]

		txs = append(txs, types.Transaction{
			Payload: txBytes,
		})
		start = end
	}

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

		if namespaceId == nsId {
			return &NSRange{
				NsId:  namespaceId,
				End:   offset,
				Start: start,
			}, nil
		}
		start = offset
	}

	return nil, fmt.Errorf("ns table not found")
}
