package outbound

// OutboundPayloadJSON represents a payload in the outbound result envelope.
type OutboundPayloadJSON struct {
	Text      string   `json:"text"`
	MediaURL  *string  `json:"mediaUrl"`
	MediaURLs []string `json:"mediaUrls,omitempty"`
}

// OutboundResultEnvelope wraps payloads, metadata, and delivery information.
type OutboundResultEnvelope struct {
	Payloads []OutboundPayloadJSON `json:"payloads,omitempty"`
	Meta     any                   `json:"meta,omitempty"`
	Delivery *OutboundDeliveryJSON `json:"delivery,omitempty"`
}

// BuildResultEnvelopeParams contains parameters for building a result envelope.
type BuildResultEnvelopeParams struct {
	Payloads        []OutboundPayloadJSON
	Meta            any
	Delivery        *OutboundDeliveryJSON
	FlattenDelivery *bool
}

// BuildResultEnvelopeResult is the result of building a result envelope.
// It can be either an OutboundResultEnvelope or an OutboundDeliveryJSON (when flattened).
type BuildResultEnvelopeResult struct {
	Envelope *OutboundResultEnvelope
	Delivery *OutboundDeliveryJSON
}

// IsFlattened returns true if the result is a flattened delivery JSON.
func (r BuildResultEnvelopeResult) IsFlattened() bool {
	return r.Delivery != nil && r.Envelope == nil
}

// BuildResultEnvelope combines payloads, meta, and delivery into a result envelope.
// If flattenDelivery is not explicitly false and there are no payloads or meta,
// it returns just the delivery JSON instead of wrapping it in an envelope.
func BuildResultEnvelope(params BuildResultEnvelopeParams) BuildResultEnvelopeResult {
	hasPayloads := params.Payloads != nil

	// Determine if we should flatten
	// Default to true (flatten) unless explicitly set to false
	shouldFlatten := params.FlattenDelivery == nil || *params.FlattenDelivery

	// If we can flatten: delivery exists, no meta, no payloads provided
	if shouldFlatten && params.Delivery != nil && params.Meta == nil && !hasPayloads {
		return BuildResultEnvelopeResult{
			Delivery: params.Delivery,
		}
	}

	// Build the full envelope
	envelope := &OutboundResultEnvelope{}

	if hasPayloads {
		envelope.Payloads = params.Payloads
	}
	if params.Meta != nil {
		envelope.Meta = params.Meta
	}
	if params.Delivery != nil {
		envelope.Delivery = params.Delivery
	}

	return BuildResultEnvelopeResult{
		Envelope: envelope,
	}
}

// ToAny converts the result to an any type suitable for JSON marshaling.
// Returns either the envelope or the flattened delivery.
func (r BuildResultEnvelopeResult) ToAny() any {
	if r.IsFlattened() {
		return r.Delivery
	}
	return r.Envelope
}
