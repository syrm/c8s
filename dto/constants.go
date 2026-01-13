package dto

import "time"

// ChannelTimeout is the timeout for channel operations to prevent deadlock.
// Used by both TUI and Docker layers for consistent behavior.
// Set to 15s to allow for slow Docker API operations (e.g., stats, logs).
const ChannelTimeout = 15 * time.Second
