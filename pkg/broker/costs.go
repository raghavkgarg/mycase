package broker

import "github.com/raghavkgarg/mycase/pkg/costs"

// CostModelForBroker returns the appropriate India-style CostModel for a given broker name.
// For US (Schwab), returns a zero-cost model since US equities have negligible costs
// ($0 commission). The actual US-specific costs (SEC fee, TAF) are tracked separately
// via costs.DefaultUS when needed.
func CostModelForBroker(brokerName string) costs.CostModel {
	switch brokerName {
	case "schwab":
		return costs.CostModel{Brokerage: 0}
	default:
		return costs.DefaultZerodha
	}
}
