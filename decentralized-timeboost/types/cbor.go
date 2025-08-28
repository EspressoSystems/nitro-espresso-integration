package decentralized_timeboost_types

type Round struct {
	Number      uint64 `cbor:"0,keyasint"`
	CommitteeId uint64 `cbor:"1,keyasint"`
}

type Block struct {
	Number  uint64 `cbor:"0,keyasint"`
	Round   uint64 `cbor:"1,keyasint"`
	Payload []byte `cbor:"2,keyasint"`
}

type BlockInfo struct {
	Num   uint64 `cbor:"0,keyasint"`
	Round Round  `cbor:"1,keyasint"`
	Hash  []byte `cbor:"2,keyasint"`
}

type Certificate struct {
	Data       BlockInfo        `cbor:"0,keyasint"`
	Commitment []byte           `cbor:"1,keyasint"`
	Signatures map[uint8][]byte `cbor:"2,keyasint"`
}

type CertifiedBlock struct {
	Version uint8       `cbor:"0,keyasint"`
	Data    Block       `cbor:"1,keyasint"`
	Cert    Certificate `cbor:"2,keyasint"`
}

type MessagePayload struct {
	Position uint64 `cbor:"pos"`
	Message  []byte `cbor:"msg"`
}
