package espressotee

type ServiceType uint8

const (
	BatchPoster ServiceType = iota
	CaffNode
)

const (
	Test ServiceType = 2 // Add testing tag at 255 to avoid collisions as this is the least "real" option for a service type.
	// Currently this doesn't work with the mock contracts, we should probably add a way to make this possible.
	// :
)
