package protocolconv

import "fmt"

// Route is the protocol conversion metadata resolved by the scheduler and
// provider service for one upstream attempt. It contains no authentication,
// endpoint, retry, or billing policy.
type Route struct {
	Source         Protocol
	IntendedTarget Protocol
	ClientModel    string
	UpstreamModel  string
	Provider       string
	AccountID      int64
}

// Validate rejects implicit or incomplete protocol routes. Model identifiers
// may be empty only for protocol operations that carry them outside the JSON
// body and populate Options.SourceModel separately.
func (r Route) Validate() error {
	if err := r.Source.Validate(); err != nil {
		return err
	}
	if err := r.IntendedTarget.Validate(); err != nil {
		return err
	}
	if r.AccountID < 0 {
		return fmt.Errorf("protocol route account ID must not be negative")
	}
	return nil
}
