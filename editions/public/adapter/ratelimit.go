package adapter

import (
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// RateLimitPolicy is the public product's client-side throttle: a coarse 50/s
// bucket. It is a runaway ceiling, not a copy of the gateway's quota — that is
// per-endpoint and per-identity (weight-based), changeable at runtime, so no
// fixed client number can track it. Normal use stays well under 50/s; the
// backend's 429 (retried with backoff) remains the real limit. 50 was chosen
// against a measured backend steady-state of ~18-31 req/s on the lightest
// endpoint: high enough never to throttle legitimate use, low enough to stop an
// agent firing hundreds of requests a second.
//
// Burst equals the rate: the gateway tolerates a large burst (~300), so there is
// no need to hold a wider client burst than the steady rate. MaxWait bounds how
// long a drained bucket makes a caller wait before it errors out, rather than
// hanging — the same guard OKX's client applies.
var RateLimitPolicy = &transport.RateLimitPolicy{
	RequestsPerSecond: 50,
	Burst:             50,
	MaxWait:           30 * time.Second,
}
