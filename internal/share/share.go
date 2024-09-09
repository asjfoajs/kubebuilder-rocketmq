// Package share defines some variables shared by different packages
package share

var (
	// GroupNum is the number of broker group
	GroupNum = 0

	// NameServersStr is the name server list
	NameServersStr = ""

	// IsNameServersStrUpdated is whether the name server list is updated
	IsNameServersStrUpdated = false

	// IsNameServersStrInitialized is whether the name server list is initialized
	IsNameServersStrInitialized = false

	// BrokerClusterName is the broker cluster name
	BrokerClusterName = ""

	// svc of controller for brokers
	ControllerAccessPoint = ""
)
