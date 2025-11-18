package types

import (
	"bytes"
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/offchainlabs/nitro/daprovider"
	"github.com/offchainlabs/nitro/daprovider/celestia/tree"
	"github.com/offchainlabs/nitro/util/containers"
)

func NewReaderForCelestia(celestiaReader CelestiaReader) *readerForCelestia {
	return &readerForCelestia{celestiaReader: celestiaReader}
}

type readerForCelestia struct {
	celestiaReader CelestiaReader
}

func (c *readerForCelestia) IsValidHeaderByte(ctx context.Context, headerByte byte) bool {
	return IsCelestiaMessageHeaderByte(headerByte)
}

// CelestiaMessageHeaderFlag indicates that this data is a Blob Pointer
// which will be used to retrieve data from Celestia
const CelestiaMessageHeaderFlag byte = 0x63

func hasBits(checking byte, bits byte) bool {
	return (checking & bits) == bits
}

func IsCelestiaMessageHeaderByte(header byte) bool {
	return hasBits(header, CelestiaMessageHeaderFlag)
}

func (c *readerForCelestia) GetProof(ctx context.Context, msg []byte) ([]byte, error) {
	return c.celestiaReader.GetProof(ctx, msg)
}

func (c *readerForCelestia) RecoverPayload(
	batchNum uint64,
	batchBlockHash common.Hash,
	sequencerMsg []byte,
) containers.PromiseInterface[daprovider.PayloadResult] {
	promise, ctx := containers.NewPromiseWithContext[daprovider.PayloadResult](context.Background())
	go func() {
		payload, _, err := c.recoverInternal(ctx, batchNum, sequencerMsg, false)
		if err != nil {
			promise.ProduceError(err)
		} else {
			promise.Produce(daprovider.PayloadResult{Payload: payload})
		}
	}()
	return promise
}

// CollectPreimages collects preimages from the DA provider given the batch header information
func (c *readerForCelestia) CollectPreimages(
	batchNum uint64,
	batchBlockHash common.Hash,
	sequencerMsg []byte,
) containers.PromiseInterface[daprovider.PreimagesResult] {
	promise, ctx := containers.NewPromiseWithContext[daprovider.PreimagesResult](context.Background())
	go func() {
		_, preimages, err := c.recoverInternal(ctx, batchNum, sequencerMsg, true)
		if err != nil {
			promise.ProduceError(err)
		} else {
			promise.Produce(daprovider.PreimagesResult{Preimages: preimages})
		}
	}()
	return promise
}

func (c *readerForCelestia) recoverInternal(
	ctx context.Context,
	batchNum uint64,
	sequencerMsg []byte,
	needPreimages bool,
) ([]byte, daprovider.PreimagesMap, error) {
	var preimages daprovider.PreimagesMap
	var preimageRecorder daprovider.PreimageRecorder
	if needPreimages {
		preimages = make(daprovider.PreimagesMap)
		preimageRecorder = daprovider.RecordPreimagesTo(preimages)
	}
	buf := bytes.NewBuffer(sequencerMsg[40:])

	header, err := buf.ReadByte()
	if err != nil {
		log.Error("Couldn't deserialize Celestia header byte", "err", err)
		return nil, nil, errors.New("tried to deserialize a message that doesn't have the Celestia header")
	}
	if !IsCelestiaMessageHeaderByte(header) {
		log.Error("Couldn't deserialize Celestia header byte", "err", errors.New("tried to deserialize a message that doesn't have the Celestia header"))
		return nil, nil, errors.New("tried to deserialize a message that doesn't have the Celestia header")
	}

	blobPointer := BlobPointer{}
	blobBytes := buf.Bytes()
	err = blobPointer.UnmarshalBinary(blobBytes)
	if err != nil {
		return nil, nil, err
	}

	payload, squareData, err := c.celestiaReader.Read(ctx, &blobPointer)
	if err != nil {
		log.Error("Failed to resolve blob pointer from celestia", "err", err)
		return nil, nil, err
	}

	if len(payload) == 0 {
		return nil, nil, errors.New("got empty payload from Celestia reader")
	}

	if preimageRecorder != nil {
		if squareData == nil {
			log.Error("squareData is nil, read from replay binary, but preimages are empty")
			return nil, nil, err
		}
		odsSize := squareData.SquareSize / 2
		rowIndex := squareData.StartRow
		for _, row := range squareData.Rows {
			treeConstructor := tree.NewConstructor(preimageRecorder, odsSize)
			root, err := tree.ComputeNmtRoot(treeConstructor, uint(rowIndex), row)
			if err != nil {
				log.Error("Failed to compute row root", "err", err)
				return nil, nil, err
			}

			rowRootMatches := bytes.Equal(squareData.RowRoots[rowIndex], root)
			if !rowRootMatches {
				log.Error("Row roots do not match", "eds row root", squareData.RowRoots[rowIndex], "calculated", root)
				log.Error("Row roots", "row_roots", squareData.RowRoots)
				return nil, nil, err
			}
			rowIndex += 1
		}

		rowsCount := len(squareData.RowRoots)
		slices := make([][]byte, rowsCount+rowsCount)
		copy(slices[0:rowsCount], squareData.RowRoots)
		copy(slices[rowsCount:], squareData.ColumnRoots)

		dataRoot := tree.HashFromByteSlices(preimageRecorder, slices)

		dataRootMatches := bytes.Equal(dataRoot, blobPointer.DataRoot[:])
		if !dataRootMatches {
			log.Error("Data Root do not match", "blobPointer data root", blobPointer.DataRoot, "calculated", dataRoot)
			return nil, nil, errors.New("data roots do not match")
		}
	}

	return payload, preimages, nil
}
