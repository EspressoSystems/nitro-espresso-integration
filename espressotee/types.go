package espressotee

type ServiceType uint8

const (
	BatchPoster ServiceType = iota
	CaffNode
)
